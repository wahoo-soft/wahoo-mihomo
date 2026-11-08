package outboundgroup

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/stretchr/testify/require"
)

type poolProxy struct {
	C.Proxy
	name  string
	delay atomic.Uint32
	alive atomic.Bool
}

func sample(name string, delay uint16) *poolProxy {
	p := &poolProxy{name: name}
	p.delay.Store(uint32(delay))
	p.alive.Store(true)
	return p
}
func (p *poolProxy) Name() string                      { return p.name }
func (p *poolProxy) AliveForTestUrl(string) bool       { return p.alive.Load() }
func (p *poolProxy) LastDelayForTestUrl(string) uint16 { return uint16(p.delay.Load()) }

type poolProvider struct {
	P.ProxyProvider
	mu      sync.Mutex
	proxies []C.Proxy
	version uint32
}

func (p *poolProvider) Version() uint32 { p.mu.Lock(); defer p.mu.Unlock(); return p.version }
func (p *poolProvider) Proxies() []C.Proxy {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]C.Proxy(nil), p.proxies...)
}
func (p *poolProvider) Touch() {}
func (p *poolProvider) replace(proxies ...C.Proxy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.proxies = proxies
	p.version++
}
func group(t *testing.T, tolerance uint16, proxies ...C.Proxy) (*URLTest, *poolProvider) {
	t.Helper()
	pd := &poolProvider{proxies: proxies, version: 1}
	u, err := NewURLTest(GroupCommonOption{Name: "pool", URL: testUrl}, URLTestOption{Tolerance: tolerance}, sample("empty", 0xffff), []P.ProxyProvider{pd})
	require.NoError(t, err)
	return u, pd
}
func TestURLTestInitialPool(t *testing.T) {
	for _, tc := range []struct {
		tolerance uint16
		delays    []uint16
		eligible  int
	}{
		{30, []uint16{40, 50, 65, 90}, 3}, {0, []uint16{40, 50, 65, 90}, 1},
		{0, []uint16{40, 40, 90}, 2}, {30, []uint16{65520, 65530, 65534}, 3},
		{30, []uint16{40, 41, 42, 43, 44, 45, 46, 47, 48, 49}, 10},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			var proxies []C.Proxy
			for i, d := range tc.delays {
				proxies = append(proxies, sample(fmt.Sprint(i), d))
			}
			seen := map[C.Proxy]int{}
			for i := 0; i < 4000; i++ {
				u, _ := group(t, tc.tolerance, proxies...)
				seen[u.fast(false)]++
			}
			require.Len(t, seen, tc.eligible)
			for i, p := range proxies {
				if i < tc.eligible {
					require.Greater(t, seen[p], 0)
				} else {
					require.Zero(t, seen[p])
				}
			}
		})
	}
}
func TestURLTestStickyFailoverRecoveryAndReplacement(t *testing.T) {
	a, b, c := sample("a", 40), sample("b", 50), sample("c", 90)
	u, pd := group(t, 30, a, b, c)
	current := u.fast(false).(*poolProxy)
	current.delay.Store(120)
	for i := 0; i < 20; i++ {
		require.Same(t, current, u.fast(true))
	}
	current.alive.Store(false)
	next := u.fast(false)
	require.NotSame(t, current, next)
	require.True(t, next.AliveForTestUrl(testUrl))
	current.alive.Store(true)
	current.delay.Store(1)
	require.Same(t, next, u.fast(false))
	replacement := sample(next.Name(), 200)
	pd.replace(current, replacement)
	require.Same(t, replacement, u.fast(false))
	pd.replace(current)
	require.Same(t, current, u.fast(false))
	pd.replace(sample("renamed", 20))
	require.Equal(t, "renamed", u.Now())
	pd.replace()
	require.Equal(t, "empty", u.Now())
	pd.replace(a)
	require.Same(t, a, u.fast(false))
}
func TestURLTestNoUsableSample(t *testing.T) {
	unknown, failed, zero := sample("unknown", 65535), sample("failed", 50), sample("zero", 0)
	failed.alive.Store(false)
	u, pd := group(t, 30, unknown, failed, zero)
	require.Same(t, unknown, u.fast(false))
	require.Nil(t, u.fastNode)
	zero.delay.Store(25)
	require.Same(t, zero, u.fast(false))
	zero.alive.Store(false)
	require.Same(t, unknown, u.fast(false))
	require.Nil(t, u.fastNode)
	pd.replace(failed)
	require.Same(t, failed, u.fast(false)) // upstream all-failed behavior
	require.Nil(t, u.fastNode)
}
func TestURLTestManualSelection(t *testing.T) {
	a, b := sample("a", 40), sample("b", 200)
	u, _ := group(t, 30, a, b)
	require.NoError(t, u.Set("b"))
	require.Same(t, b, u.fast(false))
	b.alive.Store(false)
	require.Same(t, a, u.fast(false))
	b.alive.Store(true)
	require.Same(t, b, u.fast(false)) // upstream explicit override
	u.ForceSet("")
	require.Same(t, b, u.fast(false))
	require.Error(t, u.Set("missing"))
}
func TestURLTestConcurrentHealthSelectionAndProviderRefresh(t *testing.T) {
	a, b := sample("a", 40), sample("b", 50)
	u, pd := group(t, 30, a, b)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				switch worker {
				case 0:
					b.alive.Store(i%2 == 0)
					a.delay.Store(uint32(40 + i))
				case 1:
					pd.replace(a, b)
				case 2:
					u.ForceSet("")
				case 3:
					_, err := json.Marshal(u)
					require.NoError(t, err)
				default:
					require.NotNil(t, u.fast(true))
				}
			}
		}(worker)
	}
	wg.Wait()
}
