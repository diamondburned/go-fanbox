package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	SessionID string `required:"true" envconfig:"SESSION_ID"`
	// DEST_DIR is the directory to download images to.
	DestDir string `default:"." split_words:"true"`
	// MAX_PARALLEL is the maximum parallel connections to make for downloading.
	// It defaults to the number of threads.
	MaxParallel int `split_words:"true"`
	// MAX_RETRIES is the number of retries to hit the Fanbox server. 0 means to
	// not retry.
	MaxRetries int `default:"4" split_words:"true"`
	// MAX_PAGE_BEHIND is the number of pages to look back when we don't have
	// all posts downloaded.
	MaxPageBehind int `default:"2" split_words:"true"`
	// POLL_FREQUENCY is the frequency to poll for new posts.
	PollFrequency time.Duration `default:"5m" split_words:"true"`
	// ALLOW_FILE_EXTS is the list of allowed file extensions without the
	// trailing dot for all files. This does not include images.
	AllowFileExts CommaWords `default:"gif,mp4" split_words:"true"`
}

func init() {
	rand.Seed(time.Now().UnixNano())
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

	if err := app.poll(true); err != nil {
		log.Fatalln("failed to run the initial poll:", err)
	}

	for range time.Tick(cfg.PollFrequency) {
		if err := app.poll(false); err != nil {
			log.Println("failed to periodically poll:", err)
		}
	}
}

type app struct {
	Config
	session *fanbox.Session
	sema    *semaphore.Weighted
}

func (c *app) poll(fetchAll bool) (err error) {
	var lastPage *fanbox.Page
	var page = 0

PageLoop:
	for page < c.MaxPageBehind {
		log.Printf("Scanning page %d.\n", page)

		switch {
		case page == 0:
			lastPage, err = c.session.SupportingPosts()

		case lastPage.Body.NextURL != "":
			lastPage, err = c.session.PostsFromURL(lastPage.Body.NextURL)

		default:
			log.Println("There is no next page.")
			break PageLoop // no next page, skip.
		}

		if err != nil {
			return fmt.Errorf("failed to get supporting posts page %d: %w", page, err)
		}

		lastFetched, err := c.downloadPage(lastPage)
		if err != nil {
			return fmt.Errorf("failed to download page %d: %w", page, err)
		}

		if !fetchAll && lastFetched {
			break PageLoop
		}

		log.Printf("Page %d has last item unfetched or is initial fetch; continuing.", page)
		page++
	}

	log.Printf("Finished fetching up until page %d.", page)

	return nil
}

func (c *app) downloadPage(page *fanbox.Page) (lastFetched bool, err error) {
	for _, item := range page.Body.Items {
		var urls []string
		var text string

		switch body := item.Body.(type) {
		case *fanbox.ImageBody:
			urls = make([]string, len(body.Images))
			text = body.Text

			for i, image := range body.Images {
				urls[i] = image.OriginalURL
			}

		case *fanbox.FileBody:
			urls = make([]string, 0, len(body.Files))
			text = body.Text

			for _, file := range body.Files {
				if c.AllowFileExts.Include(file.Extension) {
					urls = append(urls, file.URL)
				}
			}

		default:
			continue
		}

		if len(urls) == 0 {
			continue
		}

		dir := filepath.Join(
			c.DestDir,
			sanitizePath(item.CreatorID),
			fmt.Sprintf(
				"%s: %s",
				time.Time(item.PublishedDateTime).Format("2006-01-02"),
				sanitizePath(item.Title),
			),
		)

		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return false, errors.Wrap(err, "failed to mkdir -p for item")
		}

		var fetchedItems int

		for _, url := range urls {
			oURL := url
			name := filepath.Base(oURL)

			// Check if we already have the image.
			_, err := os.Stat(filepath.Join(dir, name))
			if err == nil {
				fetchedItems++
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

				if err := downloadFile(dir, name, r); err != nil {
					log.Println("failed to write image file:", err)
				}
			}()
		}

		text = fmt.Sprintf("%s\n\n%s", item.URL(), text)

		if err := writeText(dir, "info", text); err != nil {
			log.Println("failed to write info file:", err)
		}

		// set on each loop, use last iteration
		lastFetched = fetchedItems == len(urls)
	}

	return
}

func downloadFile(dir, file string, r io.Reader) error {
	dst := filepath.Join(dir, file)
	tmp := filepath.Join(dir, tmpFilename())

	return writeTmp(dst, tmp, r)
}

func writeText(dir, file, text string) error {
	dst := filepath.Join(dir, file)
	tmp := filepath.Join(dir, tmpFilename())

	_, err := os.Stat(filepath.Join(dir, file))
	if err == nil {
		return nil
	}

	return writeTmp(dst, tmp, strings.NewReader(text))
}

func writeTmp(dst, tmp string, r io.Reader) error {
	f, err := os.Create(tmp)
	if err != nil {
		return errors.Wrap(err, "failed to create tmp image file")
	}

	_, err = io.Copy(f, r)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return errors.Wrap(err, "failed to download image to tmp file")
	}

	f.Close()

	if err := os.Rename(tmp, dst); err != nil {
		return errors.Wrap(err, "failed to restore image tmp to dst")
	}

	return nil
}

func tmpFilename() string {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint64(buf[0:], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint32(buf[8:], rand.Uint32())

	return ".tmp." + base64.RawURLEncoding.EncodeToString(buf)
}

var sanitizer = strings.NewReplacer(
	"/", " ∕ ",
	"\x00", "",
)

func sanitizePath(part string) string {
	return sanitizer.Replace(part)
}
