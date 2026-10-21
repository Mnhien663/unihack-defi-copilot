package database

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func ConnectDB() (*sql.DB, error) {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	// Ping kiem tra ket noi
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func InitTables(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS chat_sessions (
		session_id SERIAL PRIMARY KEY,
		wallet_address VARCHAR(255) NOT NULL,
		role VARCHAR(50) NOT NULL,
		message TEXT NOT NULL, 
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("Loi khi tao bang chat_sessions: %v", err)
	}

	fmt.Println("Da kiem tra/tao bang chat_sessions thanh cong!")
	return nil
}
