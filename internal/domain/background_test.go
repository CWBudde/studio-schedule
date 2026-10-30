package domain_test

import (
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

var (
	jpegBytes = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10}
	pngBytes  = []byte("\x89PNG\r\n\x1a\n....")
	webpBytes = []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")
)

func TestBackgroundValidate(t *testing.T) {
	t.Parallel()

	for typ, data := range map[string][]byte{
		domain.BackgroundJPEG: jpegBytes, domain.BackgroundPNG: pngBytes, domain.BackgroundWebP: webpBytes,
	} {
		if err := (domain.Background{ContentType: typ, Data: data}).Validate(); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}

	// Exactly the limit is fine.
	full := append(append([]byte{}, jpegBytes...), make([]byte, domain.MaxBackgroundBytes-len(jpegBytes))...)
	if err := (domain.Background{ContentType: domain.BackgroundJPEG, Data: full}).Validate(); err != nil {
		t.Errorf("1 MiB: %v", err)
	}

	for name, tc := range map[string]struct {
		b     domain.Background
		field string
	}{
		"gif":           {domain.Background{ContentType: "image/gif", Data: []byte("GIF89a")}, "contentType"},
		"no type":       {domain.Background{Data: jpegBytes}, "contentType"},
		"empty":         {domain.Background{ContentType: domain.BackgroundJPEG}, "data"},
		"1 MiB + 1":     {domain.Background{ContentType: domain.BackgroundJPEG, Data: append(full, 0)}, "data"},
		"png as jpeg":   {domain.Background{ContentType: domain.BackgroundJPEG, Data: pngBytes}, "data"},
		"riff not webp": {domain.Background{ContentType: domain.BackgroundWebP, Data: []byte("RIFF\x10\x00\x00\x00WAVEfmt ")}, "data"},
		"short webp":    {domain.Background{ContentType: domain.BackgroundWebP, Data: []byte("RIFF")}, "data"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var fe *domain.FieldError
			if err := tc.b.Validate(); !errors.As(err, &fe) || fe.Field != tc.field || !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("want a field error on %s, got %v", tc.field, err)
			}
		})
	}
}

func TestBackgroundDigest(t *testing.T) {
	t.Parallel()

	if got := (domain.Background{}).Digest(); got != "" {
		t.Errorf("zero digest = %q", got)
	}

	if !(domain.Background{}).IsZero() || (domain.Background{ContentType: domain.BackgroundJPEG, Data: jpegBytes}).IsZero() {
		t.Error("IsZero")
	}

	b := domain.Background{ContentType: domain.BackgroundJPEG, Data: jpegBytes}
	if got, want := b.Digest(), (domain.Logo{ContentType: domain.LogoJPEG, Data: jpegBytes}).Digest(); got != want {
		t.Errorf("digest %q, want the logo's %q", got, want)
	}

	if b.Digest() == (domain.Background{ContentType: domain.BackgroundPNG, Data: jpegBytes}).Digest() {
		t.Error("the digest must change with the content type")
	}

	if !(domain.BackgroundInfo{}).IsZero() || (domain.BackgroundInfo{Digest: b.Digest()}).IsZero() {
		t.Error("BackgroundInfo.IsZero")
	}
}
