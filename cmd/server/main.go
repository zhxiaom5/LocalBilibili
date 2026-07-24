package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"localbilibili/internal/server"
	"localbilibili/web"
)

func main() {
	addr := getenv("LOCALBILIBILI_ADDR", ":8080")
	dbPath := getenv("LOCALBILIBILI_DB", "localbilibili.db")
	webDir := os.Getenv("LOCALBILIBILI_WEB_DIR")
	configPath := getenv("LOCALBILIBILI_CONFIG", "localbilibili.config.json")

	webFS, err := web.Dist()
	if err != nil {
		log.Fatal(err)
	}
	defaultDBDir := filepath.Dir(dbPath)
	app, err := server.NewConfiguredAppWithAssets(configPath, defaultDBDir, server.WebAssets{Dir: webDir, FS: webFS})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	log.Printf("LocalBilibili listening on %s", displayAddr(addr))
	if err := http.ListenAndServe(addr, app.Routes()); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func displayAddr(addr string) string {
	if addr == "" {
		return "http://localhost:8080"
	}
	if addr[0] == ':' {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
