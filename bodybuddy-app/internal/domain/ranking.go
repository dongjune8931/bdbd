package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	rankingKey = "bodybuddy:ranking"
)

// ScoreEntry represents a single score record.
type ScoreEntry struct {
	UserID   string          `json:"user_id"`
	UploadID string          `json:"upload_id"`
	Score    int             `json:"score"`
	Breakdown ScoreBreakdown `json:"breakdown"`
}

// Character represents the characters table row.
type Character struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	Level      int       `json:"level"`
	TotalScore int       `json:"total_score"`
	LastUpdated time.Time `json:"last_updated"`
}

// RankingEntry represents a single ranking item returned to the client.
type RankingEntry struct {
	Rank   int    `json:"rank"`
	UserID string `json:"user_id"`
	Score  int    `json:"score"`
}

// UpdateScore persists a new score and updates the Redis ranking.
// Returns (true, nil) if the score was newly saved, (false, nil) if it was a
// duplicate upload_id (already processed — idempotent no-op). Callers must
// skip downstream side-effects (e.g. notification) when false is returned.
func UpdateScore(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, entry ScoreEntry) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	breakdownJSON, err := json.Marshal(entry.Breakdown)
	if err != nil {
		return false, fmt.Errorf("marshalling breakdown: %w", err)
	}

	// Insert score history. upload_id has a UNIQUE constraint, so a duplicate
	// SQS redelivery produces 0 rows affected — we skip all subsequent updates.
	tag, err := tx.Exec(ctx,
		`INSERT INTO score_history (user_id, upload_id, score, score_breakdown)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (upload_id) DO NOTHING`,
		entry.UserID, entry.UploadID, entry.Score, breakdownJSON,
	)
	if err != nil {
		return false, fmt.Errorf("inserting score history: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already processed — idempotent no-op. Signal caller to skip notification.
		return false, tx.Commit(ctx)
	}

	// Update character: increment total_score and recalculate level (every 100 pts = 1 level).
	var totalScore int
	err = tx.QueryRow(ctx,
		`UPDATE characters
		 SET total_score = total_score + $2, last_updated = NOW()
		 WHERE user_id = $1
		 RETURNING total_score`,
		entry.UserID, entry.Score,
	).Scan(&totalScore)
	if err != nil {
		return false, fmt.Errorf("updating character: %w", err)
	}

	level := totalScore/100 + 1
	_, err = tx.Exec(ctx,
		`UPDATE characters SET level = $2 WHERE user_id = $1`,
		entry.UserID, level,
	)
	if err != nil {
		return false, fmt.Errorf("updating character level: %w", err)
	}

	// Mark upload as completed.
	_, err = tx.Exec(ctx,
		`UPDATE inbody_uploads SET status = 'completed', processed_at = NOW()
		 WHERE id = $1`,
		entry.UploadID,
	)
	if err != nil {
		// Non-fatal: upload record may not exist in local dev.
		_ = err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("committing transaction: %w", err)
	}

	// Update Redis ranking (ZADD — uses total score).
	err = rdb.ZAdd(ctx, rankingKey, redis.Z{
		Score:  float64(totalScore),
		Member: entry.UserID,
	}).Err()
	if err != nil {
		// Log-worthy but not fatal; DB is source of truth.
		return false, fmt.Errorf("updating redis ranking: %w", err)
	}

	return true, nil
}

// GetTopRanking returns the top-N users from the Redis sorted set.
func GetTopRanking(ctx context.Context, rdb *redis.Client, limit int64) ([]RankingEntry, error) {
	results, err := rdb.ZRevRangeWithScores(ctx, rankingKey, 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("querying ranking: %w", err)
	}

	ranking := make([]RankingEntry, 0, len(results))
	for i, z := range results {
		ranking = append(ranking, RankingEntry{
			Rank:   i + 1,
			UserID: fmt.Sprintf("%v", z.Member),
			Score:  int(z.Score),
		})
	}
	return ranking, nil
}

// GetUserScore returns the character (score + level) for a given user.
func GetUserScore(ctx context.Context, pool *pgxpool.Pool, userID string) (*Character, error) {
	var ch Character
	err := pool.QueryRow(ctx,
		`SELECT id, user_id, name, level, total_score, last_updated
		 FROM characters WHERE user_id = $1`,
		userID,
	).Scan(&ch.ID, &ch.UserID, &ch.Name, &ch.Level, &ch.TotalScore, &ch.LastUpdated)
	if err != nil {
		return nil, fmt.Errorf("querying character: %w", err)
	}
	return &ch, nil
}

// GetUserRank returns the 1-based rank of a user in the Redis sorted set.
func GetUserRank(ctx context.Context, rdb *redis.Client, userID string) (int, error) {
	rank, err := rdb.ZRevRank(ctx, rankingKey, userID).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("querying user rank: %w", err)
	}
	return int(rank) + 1, nil
}

// ScoreToString converts an int score to a string for Redis member lookup.
func ScoreToString(score int) string {
	return strconv.Itoa(score)
}
