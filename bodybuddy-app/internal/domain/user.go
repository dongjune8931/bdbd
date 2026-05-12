package domain

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// User represents the users table row.
type User struct {
	ID           string
	Email        string
	PasswordHash string
}

// CreateUser inserts a new user and returns the generated UUID.
func CreateUser(ctx context.Context, pool *pgxpool.Pool, email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	var user User
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash`,
		email, string(hash),
	).Scan(&user.ID, &user.Email, &user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	// Create initial character for the user.
	_, err = pool.Exec(ctx,
		`INSERT INTO characters (user_id, name) VALUES ($1, $2)`,
		user.ID, "My Character",
	)
	if err != nil {
		return nil, fmt.Errorf("creating character: %w", err)
	}

	return &user, nil
}

// GetUserByEmail looks up a user by email address.
func GetUserByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (*User, error) {
	var user User
	err := pool.QueryRow(ctx,
		`SELECT id, email, password_hash FROM users WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying user: %w", err)
	}
	return &user, nil
}

// CheckPassword compares a plain password against the stored bcrypt hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
