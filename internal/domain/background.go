package domain

import "bytes"

// Background is the school's background image, which themes that show one
// (ThemeWerkstatt) put behind the app (ADR 0018): a JPEG, PNG or WebP image
// of at most MaxBackgroundBytes. Like the Logo it is stored apart from the
// settings (SettingsRepo.Background).
type Background struct {
	// ContentType is BackgroundJPEG, BackgroundPNG or BackgroundWebP.
	ContentType string
	Data        []byte
}

// MaxBackgroundBytes is the largest background image, 1 MiB.
const MaxBackgroundBytes = 1 << 20

// Background content types.
const (
	BackgroundJPEG = "image/jpeg"
	BackgroundPNG  = "image/png"
	BackgroundWebP = "image/webp"
)

// IsZero reports whether there is no background.
func (b Background) IsZero() bool { return len(b.Data) == 0 }

// Digest identifies the background as Logo.Digest does a logo: the hex
// SHA-256 of its content type, a line feed and its bytes; "" for none.
func (b Background) Digest() string { return Logo(b).Digest() }

// Validate checks that the background is a JPEG, PNG or WebP image within
// the size limit whose bytes match its type. The errors are FieldErrors on
// contentType and data.
func (b Background) Validate() error {
	var matches func([]byte) bool

	switch b.ContentType {
	case BackgroundJPEG:
		matches = prefixed([]byte{0xFF, 0xD8, 0xFF})
	case BackgroundPNG:
		matches = prefixed([]byte("\x89PNG\r\n\x1a\n"))
	case BackgroundWebP:
		// A RIFF container (its size in bytes 4 to 7) of the form WEBP.
		matches = func(d []byte) bool {
			return len(d) >= 12 && bytes.HasPrefix(d, []byte("RIFF")) && string(d[8:12]) == "WEBP"
		}
	default:
		return &FieldError{Field: "contentType", Msg: "the background must be a JPEG, PNG or WebP image"}
	}

	return validate(
		on("data", len(b.Data) > 0 && len(b.Data) <= MaxBackgroundBytes, "the background must be at most 1 MiB"),
		on("data", matches(b.Data), "the background's data does not match its type"),
	)
}

func prefixed(magic []byte) func([]byte) bool {
	return func(d []byte) bool { return bytes.HasPrefix(d, magic) }
}

// BackgroundInfo describes the background without its bytes: its content
// type and its Digest. The zero BackgroundInfo is no background.
type BackgroundInfo struct {
	ContentType string
	Digest      string
}

// IsZero reports whether there is no background.
func (b BackgroundInfo) IsZero() bool { return b.Digest == "" }
