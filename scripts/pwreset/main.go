// Resets a user's password in a COPY of the database (debug sandbox only).
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/skyle/talaria/internal/auth"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", os.Args[1])
	if err != nil {
		panic(err)
	}
	defer db.Close()
	h, err := auth.HashPassword(os.Args[3])
	if err != nil {
		panic(err)
	}
	res, err := db.Exec(`UPDATE users SET password_hash = ? WHERE username = ?`, h, os.Args[2])
	if err != nil {
		panic(err)
	}
	n, _ := res.RowsAffected()
	fmt.Println("updated rows:", n)
}
