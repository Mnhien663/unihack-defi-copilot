package database 

import (
	"database/sql"
	"fmt"
)

// Khoi tao bang chat_sessions neu chua ton tai
func InitTables(db *sql.DB) error {
	// 1. Tao bang Users
	createUsersQuery := `
	CREATE TABLE IF NOT EXISTS users (
		wallet_address VARCHAR(255) PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	// 2. Tao bang Sessions
	createSessionsQuery := `
	CREATE TABLE IF NOT EXISTS sessions (
		session_id SERIAL PRIMARY KEY,
		wallet_address VARCHAR(255) NOT NULL, 
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (wallet_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);`

	// 3. Tao bang Messages 
	createMessagesQuery := `
	CREATE TABLE IF NOT EXISTS messages (
		message_id SERIAL PRIMARY KEY,
		session_id INT NOT NULL,
		role VARCHAR(50) NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
	);`

	// Thuc thi tuan tu cac cau lenh
	queries := []string{createUsersQuery, createSessionsQuery, createMessagesQuery}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("Loi khi khoi tao bang: %v", err)
		}
	}

	fmt.Println("Da khoi tao thanh cong cac bang: users, sessions, messages!")
	return nil
}

// Cau truc du lieu cho mot tin nhan chat
type ChatMessage struct {
	MessageID     int
	SessionID 	  int
	Role          string
	Content       string
	CreatedAt     string
}
