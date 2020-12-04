package fanbox

import (
	"bytes"
	"encoding/json"
	"log"
	"time"

	"github.com/pkg/errors"
)

type Page struct {
	Body PageBody `json:"body"`
}

type PageBody struct {
	Items   []Item `json:"items"`
	NextURL string `json:"nextUrl"`
}

type DateTime time.Time

func (dt *DateTime) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)

	t, err := time.Parse(time.RFC3339, string(b))
	if err != nil {
		return err
	}

	*dt = DateTime(t)
	return nil
}

type ItemBase struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Type              ItemType `json:"type"`
	CoverImageURL     string   `json:"coverImageUrl"`
	FeeRequired       int      `json:"feeRequired"`
	PublishedDateTime DateTime `json:"publishedDatetime"`
	UpdatedDateTime   DateTime `json:"updatedDatetime"`
	Excerpt           string   `json:"excerpt"`
	IsLiked           bool     `json:"isLiked"`
	LikeCount         int      `json:"likeCount"`
	CommentCount      int      `json:"commentCount"`
	User              User     `json:"user"`
	CreatorID         string   `json:"creatorId"`
	HasAdultContent   bool     `json:"hasAdultContent"`
	Status            string   `json:"status"`
}

type ItemType string

const (
	ItemTypeArticle ItemType = "article"
	ItemTypeImage   ItemType = "image"
)

type ItemBody interface {
	itemBody()
}

type Item struct {
	ItemBase
	Body ItemBody `json:"body"` // ArticleBody || ImageBody
}

func (i *Item) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &i.ItemBase); err != nil {
		return errors.Wrap(err, "failed to unmarshal item base")
	}

	var bodyContainer struct {
		Body ItemBody `json:"body"`
	}

	switch i.Type {
	case ItemTypeArticle:
		bodyContainer.Body = &ArticleBody{}
	case ItemTypeImage:
		bodyContainer.Body = &ImageBody{}
	default:
		log.Printf("Unknown item type: %q\n", i.Type) // TODO
		return nil
	}

	if err := json.Unmarshal(b, &bodyContainer); err != nil {
		return errors.Wrap(err, "failed to unmarshal into item of exact type")
	}

	i.Body = bodyContainer.Body
	return nil
}

type ImageBody struct {
	Text   string  `json:"text"`
	Images []Image `json:"images"`
}

func (ImageBody) itemBody() {}

type ArticleBody struct {
	Blocks   []ArticleBodyBlock `json:"blocks"`
	ImageMap map[string]Image   `json:"imageMap"`
	// TODO: maybe EmbedMap and FileMap
}

func (ArticleBody) itemBody() {}

type ArticleBodyBlock struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`    // Type == "p"
	ImageID string `json:"imageId,omitempty"` // Type == "image"
}

type User struct {
	UserID  string `json:"userId"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
}

type Image struct {
	ID           string `json:"id"`
	Extension    string `json:"extension"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	OriginalURL  string `json:"originalUrl"`
	ThumbnailURL string `json:"thumbnailUrl"`
}
