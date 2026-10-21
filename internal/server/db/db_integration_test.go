package db

import (
	"context"
	"os"
	"testing"
)

func TestOpenIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	conn, err := Open(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()
	if err := conn.PingContext(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
