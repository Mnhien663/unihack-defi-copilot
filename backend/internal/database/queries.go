package database 

import (
	"database/sql"
	"fmt"
)

// Chen mot tin nhan chat vao bang chat_sessions (Tao ham insertChatMessage)
func InsertChatMessage(db *sql.DB, walletAddress, role, message string) error {
	// 1. Dam bao user da ton tai (Bo qua neu da co)
	_, err := db.Exec(`INSERT INTO users (wallet_address) VALUES ($1) ON CONFLICT (wallet_address) DO NOTHING`, walletAddress)
	if err != nil {
		return fmt.Errorf("Loi khi tao bang user: %v", err)
	}

	// 2. Lay session_id gan nhat, neu chua co thi tao moi
	var sessionID int 
	err = db.QueryRow(`SELECT session_id FROM session WHERE wallet_address = $1 ORDER BY created_at DESC LIMIT 1`, walletAddress).Scan(&sessionID)
	if err == sql.ErrNoRows {
		err = db.QueryRow(`INSERT INTO sessions (wallet_address) VALUES ($1) RETURNING session_id`, walletAddress).Scan(&sessionID)
	}
	if err != nil {
		return fmt.Errorf("Loi khi xu ly session: %v", err)
	}

	// 3. Them tin nhan vao bang messages
	query := `INSERT INTO messages (session_id, role, content) VALUES ($1, $2, $3)`
	_, err = db.Exec(query, sessionID, role, message)
	if err != nil {
		return fmt.Errorf("Loi khi them tin nhan: %v", err)
	}

	return nil
}

// Truy xuat lich su chat 
func GetChatHistory(db *sql.DB, walletAddress string) ([]ChatMessage, error) {
	query := `
			SELECT m.message_id, m.session_id, m.role, m.content, m.created_at
			FROM messages m
			JOIN sessions s ON m.session_id = s.session_id
			WHERE s.wallet_address = $1
			ORDER BY m.created_at ASC`

	rows, err := db.Query(query, walletAddress)
	if err != nil {
		return nil, fmt.Errorf("Loi khi truy van lich su: %v", err)
	}
	defer rows.Close()

	var history []ChatMessage

	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.MessageID, &msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("Loi khi doc du lieu: %v", err)
		}
		history = append(history, msg)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}
