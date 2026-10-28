package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/proxmox"
	"github.com/andregoncalvespires/homeview-dash/internal/secretbox"
)

// GuestReading is a VM or container with its network rate (bytes per second,
// computed from two consecutive readings; 0 until a second reading exists).
type GuestReading struct {
	proxmox.GuestMetrics
	RateIn, RateOut float64
}

// Reading is the latest known load of one connection. When Err is set the
// figures are the last good ones (possibly empty) and At is when they were read.
type Reading struct {
	ConnID   int64
	ConnName string
	At       time.Time
	Err      string
	Nodes    []proxmox.NodeMetrics
	Storages []proxmox.StorageMetrics
	Guests   []GuestReading
}

type sample struct {
	in, out int64
	at      time.Time
}

// readingTimeout bounds one connection's refresh so a dead server cannot stall
// the page for everybody.
const readingTimeout = 8 * time.Second

// Readings returns the load of every connection, refreshing those older than
// maxAge. Nothing is polled while nobody asks, and concurrent callers share one
// refresh. A failing connection keeps its last figures with Err set.
func (s *Service) Readings(ctx context.Context, maxAge time.Duration) []Reading {
	s.mm.Lock()
	defer s.mm.Unlock()
	if s.cache == nil {
		s.cache, s.prev = map[int64]*Reading{}, map[string]sample{}
	}
	conns, err := s.st.ListConnections(ctx)
	if err != nil {
		s.log.Warn("metrics: list connections", "err", err)
		return nil
	}
	now := s.Now()
	alive := map[int64]bool{}
	var wg sync.WaitGroup
	var wm sync.Mutex // guards writes to the cache from the goroutines
	for _, c := range conns {
		alive[c.ID] = true
		if r := s.cache[c.ID]; r != nil && r.ConnName == c.Name && now.Sub(r.At) < maxAge {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rd := s.read(ctx, c.ID, c.Name, c.URL, c.TokenID, c.SecretSealed, c.Fingerprint, c.Insecure, now)
			wm.Lock()
			if old := s.cache[c.ID]; rd.Err != "" && old != nil && old.ConnName == c.Name {
				old.Err, old.At = rd.Err, rd.At // keep the last good figures
			} else {
				s.cache[c.ID] = rd
			}
			wm.Unlock()
		}()
	}
	wg.Wait()
	for id := range s.cache {
		if !alive[id] {
			delete(s.cache, id)
		}
	}
	out := make([]Reading, 0, len(conns))
	for _, c := range conns {
		if r := s.cache[c.ID]; r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func (s *Service) read(ctx context.Context, id int64, name, url, tokenID, sealed, fp string, insecure bool, now time.Time) *Reading {
	rd := &Reading{ConnID: id, ConnName: name, At: now}
	fail := func(err error) *Reading {
		s.log.Debug("metrics read failed", "connection", name, "err", err)
		rd.Err = cleanLabel(err.Error(), 200, "error")
		return rd
	}
	secret, err := secretbox.Open(s.secret, sealed)
	if err != nil {
		return fail(fmt.Errorf("cannot decrypt the stored token"))
	}
	cl, err := s.NewClient(proxmox.Config{URL: url, TokenID: tokenID, Secret: secret, Fingerprint: fp, Insecure: insecure})
	if err != nil {
		return fail(err)
	}
	tctx, cancel := context.WithTimeout(ctx, readingTimeout)
	defer cancel()
	m, err := cl.Metrics(tctx)
	if err != nil {
		return fail(err)
	}
	rd.Nodes, rd.Storages = m.Nodes, m.Storages
	for i := range rd.Nodes {
		rd.Nodes[i].Name = cleanLabel(rd.Nodes[i].Name, 80, "?")
	}
	for _, g := range m.Guests {
		g.Name = cleanLabel(g.Name, 80, fmt.Sprintf("%s-%d", g.Kind, g.VMID))
		g.Node = cleanLabel(g.Node, 80, "?")
		gr := GuestReading{GuestMetrics: g}
		s.pm.Lock()
		key := fmt.Sprintf("%d/%s/%s/%d", id, g.Node, g.Kind, g.VMID)
		if p, ok := s.prev[key]; ok && g.Running {
			if dt := now.Sub(p.at).Seconds(); dt > 0 && g.NetIn >= p.in && g.NetOut >= p.out {
				gr.RateIn, gr.RateOut = float64(g.NetIn-p.in)/dt, float64(g.NetOut-p.out)/dt
			}
		}
		s.prev[key] = sample{g.NetIn, g.NetOut, now}
		s.pm.Unlock()
		rd.Guests = append(rd.Guests, gr)
	}
	return rd
}
