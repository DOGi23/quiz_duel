package storage

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

// Small embedded images follow the existing catalog snapshot and need no public
// upload directory. Only raster data is accepted, never remote URLs or SVG.
func validateSubjectImage(value string) error {
	if value == "" {
		return nil
	}
	const maxBytes = 40 << 10
	errInvalid := fmt.Errorf("выберите изображение JPEG или PNG до 40 КБ и размером до 1024 × 1024")
	if len(value) > base64.StdEncoding.EncodedLen(maxBytes)+32 {
		return errInvalid
	}
	var format, encoded string
	for _, candidate := range []string{"jpeg", "png"} {
		if payload, ok := strings.CutPrefix(value, "data:image/"+candidate+";base64,"); ok {
			format, encoded = candidate, payload
			break
		}
	}
	if format == "" {
		return errInvalid
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > maxBytes {
		return errInvalid
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actual != format || config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		return errInvalid
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return errInvalid
	}
	return nil
}
