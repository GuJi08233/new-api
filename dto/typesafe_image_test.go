package dto

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Images are billed as input tokens, so they must reach the estimator as files.
// Leaving them out would let an image request pre-consume as a text-only one.
func TestTypeSafeRequestTokenCountMetaIncludesImages(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("jpeg-bytes"))
	request := TypeSafeRequest{
		Model:  "subq-snap-1.1",
		State:  json.RawMessage(`"the number row stopped working"`),
		Images: []TypeSafeImage{{MediaType: "image/jpeg", Base64: encoded}},
		Questions: map[string]TypeSafeQuestion{
			"shows_keyboard": {
				Type:         TypeSafeQuestionNoul,
				Instructions: json.RawMessage(`"Does the photo show a keyboard?"`),
			},
		},
	}

	meta := request.GetTokenCountMeta()

	assert.Equal(t, "the number row stopped working\nDoes the photo show a keyboard?", meta.CombineText)
	require.Len(t, meta.Files, 1)
	assert.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	// The estimator decodes the base64 itself, so the source keeps the encoded form.
	assert.Equal(t, encoded, meta.Files[0].Source.GetRawData())
}

func TestTypeSafeRequestValidateImages(t *testing.T) {
	validImage := TypeSafeImage{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString([]byte("png-bytes"))}
	noul := TypeSafeQuestion{Type: TypeSafeQuestionNoul, Instructions: json.RawMessage(`"urgent?"`)}

	newRequest := func(images []TypeSafeImage) TypeSafeRequest {
		return TypeSafeRequest{
			Model:     "subq-snap-1.1",
			State:     json.RawMessage(`"keyboard photo"`),
			Images:    images,
			Questions: map[string]TypeSafeQuestion{"shows_keyboard": noul},
		}
	}

	testCases := []struct {
		name    string
		images  []TypeSafeImage
		wantErr string
	}{
		{name: "no images", images: nil},
		{name: "one image", images: []TypeSafeImage{validImage}},
		{name: "at the count limit", images: []TypeSafeImage{validImage, validImage, validImage, validImage}},
		{
			name:    "over the count limit",
			images:  []TypeSafeImage{validImage, validImage, validImage, validImage, validImage},
			wantErr: "at most 4 entries",
		},
		{
			name:    "unsupported media type",
			images:  []TypeSafeImage{{MediaType: "image/gif", Base64: validImage.Base64}},
			wantErr: "mediaType must be image/png, image/jpeg, or image/webp",
		},
		{
			name:    "data url prefix",
			images:  []TypeSafeImage{{MediaType: "image/png", Base64: "data:image/png;base64," + validImage.Base64}},
			wantErr: "base64 must be the raw file bytes on one line",
		},
		{
			name:    "empty base64",
			images:  []TypeSafeImage{{MediaType: "image/png", Base64: "  "}},
			wantErr: "base64 is required",
		},
		{
			name: "over the per-image limit",
			images: []TypeSafeImage{{
				MediaType: "image/png",
				Base64:    base64.StdEncoding.EncodeToString(make([]byte, MaxTypeSafeImageBytes+1)),
			}},
			wantErr: "image over 4 MiB",
		},
		{
			// Each image is inside the per-image limit, but together they exceed
			// the total, which the upstream also refuses.
			name: "over the decoded total",
			images: []TypeSafeImage{
				{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(make([]byte, 3<<20))},
				{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(make([]byte, 3<<20))},
				{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(make([]byte, 3<<20))},
			},
			wantErr: "decoded total over 8 MiB",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := newRequest(testCase.images)
			err := request.Validate()
			if testCase.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}
