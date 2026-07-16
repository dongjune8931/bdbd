package domain

import "testing"

func TestGenerateMockOCRResultDeterministic(t *testing.T) {
	first := GenerateMockOCRResult("user-a")
	second := GenerateMockOCRResult("user-a")

	if first != second {
		t.Fatalf("expected deterministic OCR result, got %+v and %+v", first, second)
	}
}

func TestCalculateScoreFromOCRBounds(t *testing.T) {
	score := CalculateScoreFromOCR(OCRResult{
		WeightKg:         70,
		SkeletalMuscleKg: 34,
		BodyFatPercent:   18,
		BMI:              22,
	})

	if score.Total < 0 || score.Total > 100 {
		t.Fatalf("expected score within 0..100, got %d", score.Total)
	}
}

func TestParseOCRLinesKoreanLabels(t *testing.T) {
	result, err := ParseOCRLines([]OCRTextLine{
		{Text: "체중 72.4 kg", Confidence: 0.99},
		{Text: "골격근량 34.8 kg", Confidence: 0.98},
		{Text: "체지방률 18.2 %", Confidence: 0.97},
		{Text: "BMI 23.1", Confidence: 0.96},
	})
	if err != nil {
		t.Fatalf("expected parsed OCR result, got error: %v", err)
	}
	if result.WeightKg != 72.4 || result.SkeletalMuscleKg != 34.8 || result.BodyFatPercent != 18.2 || result.BMI != 23.1 {
		t.Fatalf("unexpected OCR result: %+v", result)
	}
}

func TestParseOCRLinesSplitEnglishLabels(t *testing.T) {
	result, err := ParseOCRLines([]OCRTextLine{
		{Text: "Weight", Confidence: 0.99},
		{Text: "68.5 kg", Confidence: 0.99},
		{Text: "Skeletal Muscle Mass", Confidence: 0.98},
		{Text: "31.2 kg", Confidence: 0.98},
		{Text: "Percent Body Fat", Confidence: 0.97},
		{Text: "21.7 %", Confidence: 0.97},
		{Text: "BMI", Confidence: 0.96},
		{Text: "22.4", Confidence: 0.96},
	})
	if err != nil {
		t.Fatalf("expected parsed OCR result, got error: %v", err)
	}
	if result.WeightKg != 68.5 || result.SkeletalMuscleKg != 31.2 || result.BodyFatPercent != 21.7 || result.BMI != 22.4 {
		t.Fatalf("unexpected OCR result: %+v", result)
	}
}

func TestParseOCRLinesFragmentedEnglishLabels(t *testing.T) {
	result, err := ParseOCRLines([]OCRTextLine{
		{Text: "Weight", Confidence: 0.99},
		{Text: "72.4 kg", Confidence: 0.72},
		{Text: "Skeletal", Confidence: 0.45},
		{Text: "Muscle", Confidence: 0.99},
		{Text: "Mass 34.8 kg", Confidence: 0.48},
		{Text: "Percent Body", Confidence: 0.50},
		{Text: "Fat", Confidence: 0.99},
		{Text: "18.2", Confidence: 0.72},
		{Text: "BMI", Confidence: 0.99},
		{Text: "23.1", Confidence: 0.95},
	})
	if err != nil {
		t.Fatalf("expected fragmented OCR result to parse, got error: %v", err)
	}
	if result.WeightKg != 72.4 || result.SkeletalMuscleKg != 34.8 || result.BodyFatPercent != 18.2 || result.BMI != 23.1 {
		t.Fatalf("unexpected OCR result: %+v", result)
	}
}

func TestParseOCRLinesRejectsMissingFields(t *testing.T) {
	_, err := ParseOCRLines([]OCRTextLine{{Text: "Weight 68.5 kg", Confidence: 0.99}})
	if err == nil {
		t.Fatal("expected missing field error")
	}
}
