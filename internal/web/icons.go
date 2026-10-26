package web

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif" // registers the decoder used by sniffIcon
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

const (
	uploadIconPrefix = "upload:"
	maxIcon          = 512 << 10 // bytes
	maxIconPx        = 4096      // per side
	uploadIconBudget = maxIcon + 512<<10
)

// uploadIconID returns the id in an "upload:<id>" icon value.
func uploadIconID(icon string) (int64, bool) {
	rest, ok := strings.CutPrefix(icon, uploadIconPrefix)
	if !ok || rest == "" || len(rest) > 18 {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// sniffIcon returns the content type of an acceptable icon image after
// checking that it really is one. SVG is allowed (it is served with a
// sandboxing policy and only ever shown through <img>, where it cannot run
// script); anything else that merely looks like text is refused.
func sniffIcon(data []byte) (string, error) {
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxIconPx || cfg.Height > maxIconPx {
			return "", errors.New("unreadable or oversized image")
		}
		return ct, nil
	case "image/webp":
		return ct, nil
	default:
		if isSVG(data) {
			return "image/svg+xml", nil
		}
		return "", errors.New("unsupported type " + ct)
	}
}

func isSVG(data []byte) bool {
	if bytes.Contains(data, []byte("<!ENTITY")) { // no entity tricks
		return false
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	root := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return root
		}
		if err != nil {
			return false // not well-formed
		}
		if se, ok := tok.(xml.StartElement); ok && !root {
			if se.Name.Local != "svg" {
				return false
			}
			root = true
		}
	}
}

// iconFromUpload stores the image sent in the "icon_file" field, if any, and
// returns the icon value to use ("upload:<id>"). errKey is an i18n key.
func (s *Server) iconFromUpload(r *http.Request) (ref, errKey string, err error) {
	f, _, ferr := r.FormFile("icon_file")
	if ferr != nil {
		return "", "", nil // nothing was sent
	}
	defer f.Close()
	data, rerr := io.ReadAll(io.LimitReader(f, maxIcon+1))
	if rerr != nil {
		return "", "", rerr
	}
	switch {
	case len(data) == 0:
		return "", "", nil // an empty file input
	case len(data) > maxIcon:
		return "", "err.icon_size", nil
	}
	ct, serr := sniffIcon(data)
	if serr != nil {
		return "", "err.icon_type", nil
	}
	id, derr := s.st.SaveIcon(r.Context(), ct, data, time.Now())
	if derr != nil {
		return "", "", derr
	}
	return uploadIconPrefix + strconv.FormatInt(id, 10), "", nil
}

// iconExists reports whether an "upload:<id>" value points at a stored image.
func (s *Server) iconExists(ctx context.Context, icon string) bool {
	id, ok := uploadIconID(icon)
	if !ok {
		return false
	}
	_, _, err := s.st.GetIcon(ctx, id)
	return err == nil
}

// pruneIcon deletes an uploaded icon once no service refers to it.
func (s *Server) pruneIcon(ctx context.Context, icon string) {
	id, ok := uploadIconID(icon)
	if !ok {
		return
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return
	}
	for _, sv := range services {
		if sv.Icon == icon {
			return
		}
	}
	if err := s.st.DeleteIcon(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Warn("could not remove an unused icon", "id", id, "err", err)
	}
}

// getIcon serves an uploaded icon to any signed-in user.
func (s *Server) getIcon(w http.ResponseWriter, r *http.Request, sess *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	ct, data, err := s.st.GetIcon(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=31536000, immutable") // an id never changes its image
	_, _ = w.Write(data)
}
