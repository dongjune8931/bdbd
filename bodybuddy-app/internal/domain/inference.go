package domain

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
)

var numericValuePattern = regexp.MustCompile(`[-+]?\d{1,3}(?:[.,]\d+)?`)

type ocrMetricDefinition struct {
	name   string
	labels []string
	min    float64
	max    float64
}

var ocrMetricDefinitions = []ocrMetricDefinition{
	{name: "weight", labels: []string{"체중", "weight"}, min: 30, max: 250},
	{name: "skeletal_muscle", labels: []string{"골격근량", "skeletal muscle mass", "skeletal muscle", "smm"}, min: 10, max: 100},
	{name: "body_fat_percent", labels: []string{"체지방률", "percent body fat", "body fat percentage", "pbf"}, min: 1, max: 70},
	{name: "bmi", labels: []string{"체질량지수", "bmi"}, min: 10, max: 60},
}

// OCRTextLine is a sanitized text line returned by the model runtime.
type OCRTextLine struct {
	Text       string
	Confidence float64
}

// OCRResult represents structured body-composition values extracted from an upload.
type OCRResult struct {
	WeightKg         float64 `json:"weight_kg"`
	SkeletalMuscleKg float64 `json:"skeletal_muscle_kg"`
	BodyFatPercent   float64 `json:"body_fat_percent"`
	BMI              float64 `json:"bmi"`
}

func seedFromString(s string) int64 {
	var seed int64
	for _, ch := range s {
		seed = seed*31 + int64(ch)
	}
	return seed
}

// GenerateMockOCRResult creates deterministic mock OCR values from a user ID.
// This keeps the project focused on infrastructure while producing realistic-ish
// inference output for demos and load tests.
func GenerateMockOCRResult(userID string) OCRResult {
	// #nosec G404 -- deterministic mock data generation for reproducible tests.
	r := rand.New(rand.NewSource(seedFromString(userID))) //nolint:gosec

	return OCRResult{
		WeightKg:         55 + r.Float64()*35, // 55-90kg
		SkeletalMuscleKg: 22 + r.Float64()*18, // 22-40kg
		BodyFatPercent:   10 + r.Float64()*25, // 10-35%
		BMI:              18 + r.Float64()*10, // 18-28
	}
}

// ParseOCRLines extracts the four body-composition values needed by the score
// formula. Labels and values may be on the same line or adjacent OCR lines.
func ParseOCRLines(lines []OCRTextLine) (OCRResult, error) {
	values := make(map[string]float64, len(ocrMetricDefinitions))
	for _, metric := range ocrMetricDefinitions {
		value, ok := findOCRMetric(lines, metric)
		if ok {
			values[metric.name] = value
		}
	}

	missing := make([]string, 0, len(ocrMetricDefinitions))
	for _, metric := range ocrMetricDefinitions {
		if _, ok := values[metric.name]; !ok {
			missing = append(missing, metric.name)
		}
	}
	if len(missing) > 0 {
		return OCRResult{}, fmt.Errorf("missing OCR fields: %s", strings.Join(missing, ", "))
	}

	return OCRResult{
		WeightKg:         values["weight"],
		SkeletalMuscleKg: values["skeletal_muscle"],
		BodyFatPercent:   values["body_fat_percent"],
		BMI:              values["bmi"],
	}, nil
}

func findOCRMetric(lines []OCRTextLine, metric ocrMetricDefinition) (float64, bool) {
	for start := range lines {
		window := ""
		for end := start; end < len(lines) && end <= start+2; end++ {
			window = strings.TrimSpace(window + " " + strings.ToLower(strings.TrimSpace(lines[end].Text)))
			for _, label := range metric.labels {
				labelIndex := strings.Index(window, label)
				if labelIndex < 0 {
					continue
				}
				if value, ok := firstValidNumber(window[labelIndex+len(label):], metric.min, metric.max); ok {
					return value, true
				}
			}
		}
	}
	return 0, false
}

func firstValidNumber(text string, minValue, maxValue float64) (float64, bool) {
	for _, match := range numericValuePattern.FindAllString(text, -1) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(match, ",", "."), 64)
		if err == nil && value >= minValue && value <= maxValue {
			return value, true
		}
	}
	return 0, false
}

// CalculateScoreFromOCR converts OCR/body-composition values into the existing
// 100-point game score. The formula stays intentionally simple because the
// project goal is workload orchestration and observability, not fitness science.
func CalculateScoreFromOCR(ocr OCRResult) ScoreBreakdown {
	clamp := func(v, min, max float64) float64 {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}

	muscleScore := clamp(((ocr.SkeletalMuscleKg-20)/20)*40, 0, 40)
	fatScore := clamp(30-((ocr.BodyFatPercent-10)/25)*30, 0, 30)
	bmiScore := clamp(30-math.Abs(ocr.BMI-22)*5, 0, 30)

	total := int(math.Round(muscleScore + fatScore + bmiScore))
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}

	return ScoreBreakdown{
		MuscleMassScore: muscleScore,
		FatMassScore:    fatScore,
		BMIScore:        bmiScore,
		Total:           total,
	}
}
