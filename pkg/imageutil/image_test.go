package imageutil_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/practicum/gophprofile/pkg/imageutil"
	"github.com/stretchr/testify/require"
)

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestDetectMIME_JPEG(t *testing.T) {
	data := makeJPEG(t, 10, 10)
	mime, err := imageutil.DetectMIME(data)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", mime)
}

func TestDetectMIME_PNG(t *testing.T) {
	data := makePNG(t, 8, 8)
	mime, err := imageutil.DetectMIME(data)
	require.NoError(t, err)
	require.Equal(t, "image/png", mime)
}

func TestDetectMIME_Invalid(t *testing.T) {
	_, err := imageutil.DetectMIME([]byte("not an image"))
	require.Error(t, err)
}

func TestIsAllowedMIME(t *testing.T) {
	require.True(t, imageutil.IsAllowedMIME("image/jpeg"))
	require.True(t, imageutil.IsAllowedMIME("image/png"))
	require.True(t, imageutil.IsAllowedMIME("image/webp"))
	require.False(t, imageutil.IsAllowedMIME("image/gif"))
}

func TestExtensionForMIME(t *testing.T) {
	require.Equal(t, "jpeg", imageutil.ExtensionForMIME("image/jpeg"))
	require.Equal(t, "png", imageutil.ExtensionForMIME("image/png"))
	require.Equal(t, "bin", imageutil.ExtensionForMIME("image/gif"))
}

func TestDimensions(t *testing.T) {
	data := makeJPEG(t, 32, 48)
	w, h, err := imageutil.Dimensions(data)
	require.NoError(t, err)
	require.Equal(t, 32, w)
	require.Equal(t, 48, h)
}

func TestResizeAndEncode(t *testing.T) {
	data := makeJPEG(t, 200, 200)
	img, mime, err := imageutil.Decode(data)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", mime)

	thumb := imageutil.Resize(img, 100, 100)
	require.Equal(t, 100, thumb.Bounds().Dx())
	require.Equal(t, 100, thumb.Bounds().Dy())

	jpegData, err := imageutil.EncodeJPEG(thumb, 80)
	require.NoError(t, err)
	require.NotEmpty(t, jpegData)

	pngData, err := imageutil.EncodePNG(thumb)
	require.NoError(t, err)
	require.NotEmpty(t, pngData)

	encoded, outMIME, err := imageutil.Encode(thumb, "png")
	require.NoError(t, err)
	require.Equal(t, "image/png", outMIME)
	require.NotEmpty(t, encoded)

	encoded, outMIME, err = imageutil.Encode(thumb, "jpeg")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", outMIME)
	require.NotEmpty(t, encoded)
}

func TestCreateThumbnail(t *testing.T) {
	data := makeJPEG(t, 400, 400)
	thumb, err := imageutil.CreateThumbnail(data, 100)
	require.NoError(t, err)
	w, h, err := imageutil.Dimensions(thumb)
	require.NoError(t, err)
	require.Equal(t, 100, w)
	require.Equal(t, 100, h)
}

func TestReadLimited(t *testing.T) {
	data := []byte("hello")
	got, err := imageutil.ReadLimited(bytes.NewReader(data), 10)
	require.NoError(t, err)
	require.Equal(t, data, got)

	_, err = imageutil.ReadLimited(bytes.NewReader(bytes.Repeat([]byte("a"), 20)), 10)
	require.ErrorIs(t, err, imageutil.ErrTooLarge)
}
