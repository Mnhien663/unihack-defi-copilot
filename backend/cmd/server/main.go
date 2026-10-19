package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "DeFi Copilot Backend is running!")
	})

	fmt.Println("Server đang chạy tại http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Lỗi khởi chạy server:", err)
	}
}
