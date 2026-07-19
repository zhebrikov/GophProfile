package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var allowedMIME = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

func DetectMIME(data []byte) (string, error) {
	mime := http.DetectContentType(data)
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = mime[:idx]
	}
	if _, ok := allowedMIME[mime]; !ok {
		return "", fmt.Errorf("unsupported mime type: %s", mime)
	}
	return mime, nil
}

func IsAllowedMIME(mime string) bool {
	_, ok := allowedMIME[mime]
	return ok
}

func ExtensionForMIME(mime string) string {
	if ext, ok := allowedMIME[mime]; ok {
		return ext
	}
	return "bin"
}

func Decode(data []byte) (image.Image, string, error) {
	mime, err := DetectMIME(data)
	if err != nil {
		return nil, "", err
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	_ = format
	return img, mime, nil
}

func Dimensions(data []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func Resize(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Encode(img image.Image, format string) ([]byte, string, error) {
	switch strings.ToLower(format) {
	case "png", "image/png":
		data, err := EncodePNG(img)
		return data, "image/png", err
	case "webp", "image/webp":
		// WebP encode is not in stdlib; fall back to JPEG for output conversion.
		data, err := EncodeJPEG(img, 85)
		return data, "image/jpeg", err
	default:
		data, err := EncodeJPEG(img, 85)
		return data, "image/jpeg", err
	}
}

func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

var ErrTooLarge = fmt.Errorf("file too large")

func CreateThumbnail(data []byte, size int) ([]byte, error) {
	img, _, err := Decode(data)
	if err != nil {
		return nil, err
	}
	thumb := Resize(img, size, size)
	return EncodeJPEG(thumb, 85)
}
