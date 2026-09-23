package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/webauthn"
)

func AddWebAuthnCredential(userID int, credential webauthn.Credential, name string) error {
	database, err := getDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}

	_, err = database.Exec(
		"INSERT INTO webauthn_credentials (id, user_id, credential, name) VALUES ($1, $2, $3, $4)",
		credential.ID, userID, string(data), name,
	)
	if err != nil {
		return fmt.Errorf("insert webauthn credential: %w", err)
	}
	return nil
}

func UpdateWebAuthnCredential(credential webauthn.Credential) error {
	database, err := getDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}

	_, err = database.Exec(
		"UPDATE webauthn_credentials SET credential = $1, last_used_at = CURRENT_TIMESTAMP WHERE id = $2",
		string(data), credential.ID,
	)
	if err != nil {
		return fmt.Errorf("update webauthn credential: %w", err)
	}
	return nil
}

func GetWebAuthnCredentialsByUserID(userID int) ([]webauthn.Credential, error) {
	database, err := getDB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	rows, err := database.Query("SELECT credential FROM webauthn_credentials WHERE user_id = $1", userID)
	if err != nil {
		return nil, fmt.Errorf("query webauthn credentials: %w", err)
	}
	defer rows.Close()

	var credentials []webauthn.Credential
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(raw), &cred); err != nil {
			return nil, fmt.Errorf("unmarshal credential: %w", err)
		}
		credentials = append(credentials, cred)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credentials: %w", err)
	}
	return credentials, nil
}

func GetWebAuthnUserIDByCredentialID(credentialID []byte) (int, error) {
	database, err := getDB()
	if err != nil {
		return 0, fmt.Errorf("open database: %w", err)
	}
	var userID int
	err = database.QueryRow(
		"SELECT user_id FROM webauthn_credentials WHERE id = $1",
		credentialID,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("webauthn credential not found: %w", err)
	}
	if err != nil {
		return 0, fmt.Errorf("query webauthn credential: %w", err)
	}
	return userID, nil
}

func DeleteWebAuthnCredential(credentialID []byte, userID int) (bool, error) {
	database, err := getDB()
	if err != nil {
		return false, fmt.Errorf("open database: %w", err)
	}
	result, err := database.Exec(
		"DELETE FROM webauthn_credentials WHERE id = $1 AND user_id = $2",
		credentialID, userID,
	)
	if err != nil {
		return false, fmt.Errorf("delete webauthn credential: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return rows > 0, nil
}

func ListUserWebAuthnCredentials(userID int) ([]WebAuthnCredentialInfo, error) {
	database, err := getDB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	rows, err := database.Query(
		"SELECT id, name, created_at, last_used_at FROM webauthn_credentials WHERE user_id = $1 ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query webauthn credentials: %w", err)
	}
	defer rows.Close()

	var infos []WebAuthnCredentialInfo
	for rows.Next() {
		var info WebAuthnCredentialInfo
		if err := rows.Scan(&info.ID, &info.Name, &info.CreatedAt, &info.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan credential info: %w", err)
		}
		infos = append(infos, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credential infos: %w", err)
	}
	return infos, nil
}

type WebAuthnCredentialInfo struct {
	ID         []byte  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}
