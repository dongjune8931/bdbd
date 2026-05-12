package domain

import (
	"math/rand"
)

// ScoreBreakdown holds the components of an inbody score calculation.
type ScoreBreakdown struct {
	MuscleMassScore float64 `json:"muscle_mass_score"`
	FatMassScore    float64 `json:"fat_mass_score"`
	BMIScore        float64 `json:"bmi_score"`
	Total           int     `json:"total"`
}

// CalculateMockScore generates a deterministic-ish score from a user-ID-based seed.
// In production this would call a real OCR result parser.
// Using userID as seed ensures the same user gets reproducible scores in test runs.
func CalculateMockScore(userID string) ScoreBreakdown {
	// Derive a numeric seed from the user ID string.
	var seed int64
	for _, ch := range userID {
		seed = seed*31 + int64(ch)
	}

	// #nosec G404 — intentional use of weak RNG for mock score generation
	r := rand.New(rand.NewSource(seed)) //nolint:gosec

	muscle := r.Float64() * 40  // 0–40
	fat := r.Float64() * 30     // 0–30 (higher fat = lower score, inverted below)
	bmi := r.Float64() * 30     // 0–30

	// Invert fat component: lower body fat = higher score.
	fatScore := 30 - fat

	total := int(muscle + fatScore + bmi)
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}

	return ScoreBreakdown{
		MuscleMassScore: muscle,
		FatMassScore:    fatScore,
		BMIScore:        bmi,
		Total:           total,
	}
}
