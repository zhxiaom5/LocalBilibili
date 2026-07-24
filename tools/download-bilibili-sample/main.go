package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"localbilibili/internal/server"
)

func main() {
	spaceURL := flag.String("url", "", "Bilibili space or video URL")
	output := flag.String("output", filepath.Join("samples", "bilibili"), "output directory")
	cookie := flag.String("cookie", "", "optional Bilibili Cookie")
	flag.Parse()
	if strings.TrimSpace(*spaceURL) == "" {
		log.Fatal("-url is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := os.MkdirAll(*output, 0o755); err != nil {
		log.Fatal(err)
	}
	downloader := server.BilibiliDownloader{}
	result, err := downloader.UpdateCreator(ctx, server.DownloadTask{
		ProfileURL: *spaceURL,
		OutputDir:  *output,
		Cookie:     *cookie,
		Progress: func(progress server.DownloadProgress) {
			if progress.Message != "" {
				fmt.Printf("%s processed=%d/%d media=%d skipped=%d\n", progress.Message, progress.ProcessedWorks, progress.TotalWorks, progress.DownloadedMedia, progress.SkippedWorks)
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("creator=%s added=%d output=%s\n", result.CreatorName, result.AddedWorks, *output)
}
