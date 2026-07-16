import base64
import logging
import os
import time
from contextlib import asynccontextmanager
from typing import Annotated

import cv2
import easyocr
import numpy as np
import torch
from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel, Field
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Histogram, generate_latest


logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO").upper(),
    format='{"time":"%(asctime)s","level":"%(levelname)s","service":"ocr-runtime","msg":"%(message)s"}',
)
logger = logging.getLogger("ocr-runtime")

MODEL_VERSION = os.getenv("OCR_MODEL_VERSION", "easyocr-1.7.2-ko-en")
MODEL_STORAGE = os.getenv("OCR_MODEL_STORAGE", "/models")
DEVICE = os.getenv("OCR_DEVICE", "cpu").lower()
MAX_IMAGE_BYTES = int(os.getenv("OCR_MAX_IMAGE_BYTES", str(10 * 1024 * 1024)))
LANGUAGES = [item.strip() for item in os.getenv("OCR_LANGUAGES", "ko,en").split(",") if item.strip()]

REQUESTS = Counter(
    "bodybuddy_ocr_runtime_requests_total",
    "OCR runtime requests by result and accelerator.",
    ["result", "accelerator"],
)
DURATION = Histogram(
    "bodybuddy_ocr_runtime_duration_seconds",
    "OCR model inference latency.",
    ["accelerator"],
    buckets=(0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 21),
)


class OCRRequest(BaseModel):
    image_base64: Annotated[str, Field(min_length=4)]


class OCRLine(BaseModel):
    text: str
    confidence: float


class OCRResponse(BaseModel):
    lines: list[OCRLine]
    accelerator: str
    model_version: str
    inference_ms: int


def accelerator_name() -> str:
    if DEVICE == "cuda":
        if not torch.cuda.is_available():
            raise RuntimeError("OCR_DEVICE=cuda but CUDA is unavailable")
        return torch.cuda.get_device_name(0)
    return "cpu"


def decode_image(encoded: str) -> np.ndarray:
    try:
        raw = base64.b64decode(encoded, validate=True)
    except (ValueError, TypeError) as exc:
        raise ValueError("image_base64 is not valid base64") from exc

    if not raw:
        raise ValueError("image is empty")
    if len(raw) > MAX_IMAGE_BYTES:
        raise ValueError(f"image exceeds {MAX_IMAGE_BYTES} bytes")

    image = cv2.imdecode(np.frombuffer(raw, dtype=np.uint8), cv2.IMREAD_COLOR)
    if image is None:
        raise ValueError("payload is not a supported image")
    return image


@asynccontextmanager
async def lifespan(app: FastAPI):
    accelerator = accelerator_name()
    logger.info("loading OCR model version=%s accelerator=%s", MODEL_VERSION, accelerator)
    app.state.reader = easyocr.Reader(
        LANGUAGES,
        gpu=DEVICE == "cuda",
        model_storage_directory=MODEL_STORAGE,
        download_enabled=False,
        verbose=False,
    )
    app.state.accelerator = accelerator
    logger.info("OCR model ready version=%s accelerator=%s", MODEL_VERSION, accelerator)
    yield


app = FastAPI(title="BodyBuddy OCR Runtime", version="1.0.0", lifespan=lifespan)


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/metrics", include_in_schema=False)
def metrics() -> Response:
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)


@app.get("/readyz")
def readyz() -> dict[str, str]:
    if not hasattr(app.state, "reader"):
        raise HTTPException(status_code=503, detail="model is not loaded")
    return {
        "status": "ready",
        "accelerator": app.state.accelerator,
        "model_version": MODEL_VERSION,
    }


@app.post("/v1/ocr", response_model=OCRResponse)
def run_ocr(request: OCRRequest) -> OCRResponse:
    try:
        image = decode_image(request.image_base64)
    except ValueError as exc:
        REQUESTS.labels("rejected", app.state.accelerator).inc()
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    started = time.perf_counter()
    try:
        results = app.state.reader.readtext(image, detail=1, paragraph=False)
    except Exception as exc:
        REQUESTS.labels("failure", app.state.accelerator).inc()
        logger.exception("OCR inference failed")
        raise HTTPException(status_code=500, detail="OCR inference failed") from exc

    elapsed = time.perf_counter() - started
    REQUESTS.labels("success", app.state.accelerator).inc()
    DURATION.labels(app.state.accelerator).observe(elapsed)

    lines = [
        OCRLine(text=str(item[1]).strip(), confidence=float(item[2]))
        for item in results
        if len(item) >= 3 and str(item[1]).strip()
    ]
    return OCRResponse(
        lines=lines,
        accelerator=app.state.accelerator,
        model_version=MODEL_VERSION,
        inference_ms=round(elapsed * 1000),
    )
