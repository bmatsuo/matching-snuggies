package slicerjob

import (
	"encoding/json"
	"fmt"
)

// Status signals the state a job is in.
type Status int

// Jobs typically begin in Accepted and transition to Processing, followed
// by Complete.  A job may enter a Failed state from any other.
const (
	Accepted Status = iota
	Processing
	Complete
	Failed
	Cancelled
	Invalid
)

var statusStrings = []string{
	Accepted:   "accepted",
	Processing: "processing",
	Complete:   "complete",
	Failed:     "failed",
	Cancelled:  "cancelled",
	Invalid:    "INVALIDSTATUS",
}

var statusParse = map[string]Status{
	statusStrings[Accepted]:   Accepted,
	statusStrings[Processing]: Processing,
	statusStrings[Complete]:   Complete,
	statusStrings[Cancelled]:  Cancelled,
	statusStrings[Failed]:     Failed,
	statusStrings[Invalid]:    Invalid,
}

func (s Status) IsWaiting() bool {
	return s == Accepted || s == Processing
}

// IsValid returns true if s is one of the defined Status constants
// excluding Invalid.
func (s Status) IsValid() bool {
	return s >= 0 && s < Invalid
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (s *Status) UnmarshalJSON(p []byte) error {
	var str string
	err := json.Unmarshal(p, &str)
	if err != nil {
		return err
	}
	snew, err := ParseStatus(str)
	if err != nil {
		return err
	}
	*s = snew
	return nil
}

// MarshalJSON implements the json.Marshaler interaface.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// String returns the string representation of s.
func (s Status) String() string {
	return statusStrings[s]
}

// ParseString returns the Status from its string representation, str.
func ParseStatus(str string) (Status, error) {
	if s, ok := statusParse[str]; ok {
		return s, nil
	}
	return Invalid, fmt.Errorf("invalid status")
}
