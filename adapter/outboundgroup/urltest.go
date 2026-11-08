package outboundgroup

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"

	"github.com/metacubex/mihomo/common/callback"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

type URLTestOption struct {
	Tolerance uint16 `group:"tolerance,omitempty"`
}

type URLTest struct {
	*GroupBase
	selected       string
	testUrl        string
	expectedStatus string
	tolerance      uint16
	disableUDP     bool
	fastNode       C.Proxy
	selectionMu    sync.Mutex
}

func (u *URLTest) Now() string {
	return u.fast(false).Name()
}

func (u *URLTest) Set(name string) error {
	var p C.Proxy
	for _, proxy := range u.GetProxies(false) {
		if proxy.Name() == name {
			p = proxy
			break
		}
	}
	if p == nil {
		return errors.New("proxy not exist")
	}
	u.ForceSet(name)
	return nil
}

func (u *URLTest) ForceSet(name string) {
	u.selectionMu.Lock()
	defer u.selectionMu.Unlock()
	u.selected = name
}

// DialContext implements C.ProxyAdapter
func (u *URLTest) DialContext(ctx context.Context, metadata *C.Metadata) (c C.Conn, err error) {
	proxy := u.fast(true)
	c, err = proxy.DialContext(ctx, metadata)
	if err == nil {
		c.AppendToChains(u)
	} else {
		u.onDialFailed(proxy.Type(), err, u.healthCheck)
	}

	if N.NeedHandshake(c) {
		c = callback.NewFirstWriteCallBackConn(c, func(err error) {
			if err == nil {
				u.onDialSuccess()
			} else {
				u.onDialFailed(proxy.Type(), err, u.healthCheck)
			}
		})
	}

	return c, err
}

// ListenPacketContext implements C.ProxyAdapter
func (u *URLTest) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (C.PacketConn, error) {
	proxy := u.fast(true)
	pc, err := proxy.ListenPacketContext(ctx, metadata)
	if err == nil {
		pc.AppendToChains(u)
	} else {
		u.onDialFailed(proxy.Type(), err, u.healthCheck)
	}

	return pc, err
}

// Unwrap implements C.ProxyAdapter
func (u *URLTest) Unwrap(metadata *C.Metadata, touch bool) C.Proxy {
	return u.fast(touch)
}

func (u *URLTest) healthCheck() {
	u.GroupBase.healthCheck()
}

// Selection is serialized independently of health checks. Do not cache the chosen
// proxy: provider replacement and a confirmed failure must be visible immediately.
func (u *URLTest) fast(touch bool) C.Proxy {
	u.selectionMu.Lock()
	defer u.selectionMu.Unlock()

	proxies := u.GetProxies(touch)
	if len(proxies) == 0 {
		u.fastNode = nil
		return u.EmptyFallback()
	}
	// Preserve the upstream explicit API override. Automatic selection does not
	// use this field and recovered automatic nodes cannot reclaim the selection.
	for _, proxy := range proxies {
		if proxy.Name() == u.selected && proxy.AliveForTestUrl(u.testUrl) {
			u.fastNode = proxy
			return proxy
		}
	}
	for _, proxy := range proxies {
		if u.fastNode != nil && proxy.Name() == u.fastNode.Name() && proxy.AliveForTestUrl(u.testUrl) {
			// Use the new provider object, even if its name has not changed.
			u.fastNode = proxy
			return proxy
		}
	}

	type candidate struct {
		proxy C.Proxy
		delay uint16
	}
	var candidates []candidate
	minDelay := uint32(0xffff)
	for _, proxy := range proxies {
		if !proxy.AliveForTestUrl(u.testUrl) {
			continue
		}
		delay := proxy.LastDelayForTestUrl(u.testUrl)
		// Upstream uses 0xffff for missing/failed samples; zero is not a
		// successful RTT either. Never let an untested proxy enter the pool.
		if delay == 0 || delay == 0xffff {
			continue
		}
		candidates = append(candidates, candidate{proxy, delay})
		if uint32(delay) < minDelay {
			minDelay = uint32(delay)
		}
	}
	var pool []C.Proxy
	for _, candidate := range candidates {
		// Widen before adding tolerance to avoid uint16 overflow.
		if uint32(candidate.delay) <= minDelay+uint32(u.tolerance) {
			pool = append(pool, candidate.proxy)
		}
	}
	if len(pool) == 0 {
		// Keep upstream's first-proxy behavior when no result is usable, but
		// do not pin this provisional choice before the first successful test.
		u.fastNode = nil
		return proxies[0]
	}
	u.fastNode = pool[rand.Intn(len(pool))]
	return u.fastNode
}

// SupportUDP implements C.ProxyAdapter
func (u *URLTest) SupportUDP() bool {
	if u.disableUDP {
		return false
	}
	return u.fast(false).SupportUDP()
}

// IsL3Protocol implements C.ProxyAdapter
func (u *URLTest) IsL3Protocol(metadata *C.Metadata) bool {
	return u.fast(false).IsL3Protocol(metadata)
}

// MarshalJSON implements C.ProxyAdapter
func (u *URLTest) MarshalJSON() ([]byte, error) {
	all := []string{}
	for _, proxy := range u.GetProxies(false) {
		all = append(all, proxy.Name())
	}
	now := u.Now()
	u.selectionMu.Lock()
	fixed := u.selected
	u.selectionMu.Unlock()
	return json.Marshal(map[string]any{
		"type":           u.Type().String(),
		"now":            now,
		"all":            all,
		"testUrl":        u.testUrl,
		"expectedStatus": u.expectedStatus,
		"fixed":          fixed,
		"hidden":         u.Hidden(),
		"icon":           u.Icon(),
		"emptyFallback":  u.EmptyFallback().Name(),
	})
}

func (u *URLTest) Providers() []P.ProxyProvider {
	return u.providers
}

func (u *URLTest) Proxies() []C.Proxy {
	return u.GetProxies(false)
}

func (u *URLTest) URLTest(ctx context.Context, url string, expectedStatus utils.IntRanges[uint16]) (map[string]uint16, error) {
	return u.GroupBase.URLTest(ctx, u.testUrl, expectedStatus)
}

func NewURLTest(option GroupCommonOption, urlTestOption URLTestOption, emptyFallback C.Proxy, providers []P.ProxyProvider) (*URLTest, error) {
	if emptyFallback == nil {
		return nil, errors.New("empty fallback proxy not exist")
	}
	urlTest := &URLTest{
		GroupBase: NewGroupBase(GroupBaseOption{
			Name:           option.Name,
			Type:           C.URLTest,
			Hidden:         option.Hidden,
			Icon:           option.Icon,
			Filter:         option.Filter,
			ExcludeFilter:  option.ExcludeFilter,
			ExcludeType:    option.ExcludeType,
			TestTimeout:    option.TestTimeout,
			MaxFailedTimes: option.MaxFailedTimes,
			EmptyFallback:  emptyFallback,
			Providers:      providers,
		}),
		disableUDP:     option.DisableUDP,
		testUrl:        option.URL,
		expectedStatus: option.ExpectedStatus,
		tolerance:      urlTestOption.Tolerance,
	}

	return urlTest, nil
}
