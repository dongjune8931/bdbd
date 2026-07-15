import os

import easyocr


storage = os.getenv("OCR_MODEL_STORAGE", "/models")

# Keep both models in the image. EKS uses Korean + English for InBody sheets,
# while the CPU smoke test uses the smaller English recognizer.
for languages in (("ko", "en"), ("en",)):
    easyocr.Reader(
        list(languages),
        gpu=False,
        model_storage_directory=storage,
        download_enabled=True,
        verbose=True,
    )
