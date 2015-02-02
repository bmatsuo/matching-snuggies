package slicerjob

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"code.google.com/p/go-uuid/uuid"
)

type Cursor []byte

func (c *Cursor) UnmarshalJSON(js []byte) error {
	var s string
	err := json.Unmarshal(js, &s)
	if err != nil {
		return err
	}
	p, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	*c = p
	return nil
}

func (c Cursor) MarshalJSON() ([]byte, error) {
	s := base64.URLEncoding.EncodeToString(c)
	return json.Marshal(s)
}

type Page struct {
	NumItems int         `json:"num_items"`
	Cursor   Cursor      `json:"cursor,omitempty"`
	Data     interface{} `json:"data"`
}

func JobPage(cursor Cursor, jobs []*Job) *Page {
	if jobs == nil {
		jobs = []*Job{}
	}
	return &Page{
		NumItems: len(jobs),
		Cursor:   cursor,
		Data:     jobs,
	}
}

type Job struct {
	ID         string     `json:"id"`
	Status     Status     `json:"status"`
	Progress   float64    `json:"progress"`
	URL        string     `json:"url"`
	GCodeURL   string     `json:"gcode_url"`
	Created    *time.Time `json:"created_time,omitempty"`
	Updated    *time.Time `json:"updated_time,omitempty"`
	Terminated *time.Time `json:"terminated_time,omitempty"`
}

type SlicerPreset struct {
	Slicer  string   `json:"slicer"`
	Presets []string `json:"presets"`
}

// New creates a new Job with a random UUID for an ID.  If urlformat is
// non-empty the URL of the returned job is computed as
// fmt.Sprintf(urlformat,job.ID).
func New() *Job {
	job := new(Job)
	job.ID = uuid.New()
	now := time.Now()
	job.Created = &now
	return job
}
