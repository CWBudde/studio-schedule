package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// The web app manifest (ADR 0017): it makes the app installable with the
// school's name and accent. The server answers it from the settings; the
// web demo ships a static one of its own.
const (
	// ManifestPath is the manifest's path, outside BasePath.
	ManifestPath = "/manifest.webmanifest"
	// ManifestContentType is the media type of a web app manifest.
	ManifestContentType = "application/manifest+json"
	// ManifestCacheControl lets browsers keep the manifest for five
	// minutes: a new name or accent shows soon.
	ManifestCacheControl = "public, max-age=300"
	// DefaultThemeColor is the app's own accent (web/index.html), for a
	// school without one.
	DefaultThemeColor = "#7f1f35"
	// manifestBackground is the light theme's background, the splash
	// screen's color.
	manifestBackground = "#faf7f2"
)

func registerManifest(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "getWebManifest",
		Method:      http.MethodGet,
		Path:        ManifestPath,
		Summary:     "Web app manifest",
		Description: "Returns the web app manifest that makes the app installable: the school's name, its " +
			"accent as the theme color, the language and the icons.",
		Tags: []string{"settings"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The manifest.",
				Content:     map[string]*huma.MediaType{ManifestContentType: {Schema: &huma.Schema{Type: "object"}}},
			},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		},
	}, h.getWebManifest)
}

// WebManifest is a web app manifest (W3C Web Application Manifest): its
// member names are the standard's, not the API's camelCase.
type WebManifest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name"`
	Description     string         `json:"description"`
	Lang            string         `json:"lang"`
	StartURL        string         `json:"start_url"`
	Scope           string         `json:"scope"`
	Display         string         `json:"display"`
	ThemeColor      string         `json:"theme_color"`
	BackgroundColor string         `json:"background_color"`
	Icons           []ManifestIcon `json:"icons"`
}

// ManifestIcon is an icon of the manifest.
type ManifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

// ManifestIcons are the app's icons (web/public/icons), rendered from its
// favicon.
var ManifestIcons = []ManifestIcon{
	{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"},
	{Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png"},
	{Src: "/icons/icon-maskable-512.png", Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
}

// manifestDescriptions are the manifest's description by language.
var manifestDescriptions = map[string]string{
	"en": "Class schedule and booking",
	"de": "Stundenplan und Buchung",
}

// WebManifestOutput is the response of getWebManifest.
type WebManifestOutput struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

func (h *handlers) getWebManifest(ctx context.Context, _ *struct{}) (*WebManifestOutput, error) {
	s, err := h.deps.Settings.Public(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	body, err := json.Marshal(WebManifest{
		ID: "/", Name: s.SchoolName, ShortName: s.SchoolName,
		Description: cmp.Or(manifestDescriptions[s.DefaultLocale], manifestDescriptions["en"]),
		Lang:        s.DefaultLocale, StartURL: "/", Scope: "/", Display: "standalone",
		ThemeColor: cmp.Or(s.AccentColor, DefaultThemeColor), BackgroundColor: manifestBackground,
		Icons: ManifestIcons,
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &WebManifestOutput{ContentType: ManifestContentType, CacheControl: ManifestCacheControl, Body: body}, nil
}
