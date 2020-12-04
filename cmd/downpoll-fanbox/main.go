package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/diamondburned/go-fanbox/fanbox"
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"golang.org/x/sync/semaphore"
)

type Config struct {
	// SESSION_ID is the session ID to use for the Fanbox session.
	SessionID string `required:"true" envconfig:"session_id"`
	// DEST_DIR is the directory to download images to.
	DestDir string `default:"."`
	// MAX_PARALLEL is the maximum parallel connections to make for downloading.
	// It defaults to the number of threads.
	MaxParallel int
	// MAX_RETRIES is the number of retries to hit the Fanbox server. 0 means to
	// not retry.
	MaxRetries int `default:"4"`
	// MAX_PAGE_BEHIND is the number of pages to look back when we don't have
	// all posts downloaded.
	MaxPageBehind int `default:"2"`
	// POLL_FREQUENCY is the frequency to poll for new posts.
	PollFrequency time.Duration `default:"5m" split_words:"true"`
}

func main() {
	var cfg = Config{
		MaxParallel: runtime.GOMAXPROCS(-1),
	}

	if err := envconfig.Process("fanbox", &cfg); err != nil {
		log.Fatalln("erroneous env var:", err)
	}

	session := fanbox.New(cfg.SessionID)
	session.Retries = cfg.MaxRetries

	app := &app{
		Config:  cfg,
		session: session,
		sema:    semaphore.NewWeighted(int64(cfg.MaxRetries)),
	}

	if err := app.poll(); err != nil {
		log.Fatalln("failed to run the initial poll:", err)
	}

	for range time.Tick(cfg.PollFrequency) {
		if err := app.poll(); err != nil {
			log.Println("failed to periodically poll:", err)
		}
	}
}

type app struct {
	Config
	session *fanbox.Session
	sema    *semaphore.Weighted
}

func (c *app) poll() (err error) {
	var lastPage *fanbox.Page

	for i := 0; i < c.MaxPageBehind; i++ {
		if i == 0 {
			lastPage, err = c.session.SupportingPosts()
		} else {
			lastPage, err = c.session.PostsFromURL(lastPage.Body.NextURL)
		}

		if err != nil {
			return fmt.Errorf("failed to get supporting posts page %d: %w", i, err)
		}

		lastFetched, err := c.downloadPage(lastPage)
		if err != nil {
			return fmt.Errorf("failed to download page %d: %w", i, err)
		}

		if lastFetched {
			log.Printf("Finished fetching up until page %d.", i)
			break
		}

		log.Printf("Page %d has last item unfetched; continuing.", i)
	}

	return nil
}

func (c *app) downloadPage(page *fanbox.Page) (lastFetched bool, err error) {
	for _, item := range page.Body.Items {
		if item.Type != fanbox.ItemTypeImage {
			continue
		}

		body := item.Body.(*fanbox.ImageBody)

		// Make a path-safe artist name.
		dir := filepath.Join(c.DestDir, sanitizePath(item.CreatorID), sanitizePath(item.Title))
		// Ensure directory is made.
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return false, errors.Wrap(err, "failed to mkdir -p for artist")
		}

		var fetchedImages int

		for _, image := range body.Images {
			oURL := image.OriginalURL
			path := filepath.Join(dir, filepath.Base(oURL))

			// Check if we already have the image.
			_, err := os.Stat(path)
			if err == nil {
				fetchedImages++
				continue
			}

			// Acquire a semaphore outside instead so we don't overwhelm the
			// Pixiv server too much.
			c.sema.Acquire(context.Background(), 1)

			go func() {
				defer c.sema.Release(1)

				r, err := c.session.Download(oURL)
				if err != nil {
					log.Println("failed to download image:", err)
					return
				}
				defer r.Close()

				f, err := os.Create(path)
				if err != nil {
					log.Println("failed to create image file:", err)
					return
				}
				defer f.Close()

				_, err = io.Copy(f, r)
				if err != nil {
					log.Println("failed to download image to file:", err)
					os.Remove(path)
				}
			}()
		}

		// set on each loop, use last iteration
		lastFetched = fetchedImages == len(body.Images)
	}

	return
}

var sanitizer = strings.NewReplacer(
	"/", " ∕ ",
	"\x00", "",
)

func sanitizePath(part string) string {
	return sanitizer.Replace(part)
}
