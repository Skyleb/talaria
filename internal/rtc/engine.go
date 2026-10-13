// Package rtc implements the embedded SFU for voice/video/screen share.
//
// Topology: every participant maintains two PeerConnections to the server —
// a publisher PC (client offers; carries mic/cam/screen up) and a subscriber
// PC (server offers; carries everyone else's tracks down). The split means
// neither side ever has offer glare. All media for all peers is muxed over ONE
// UDP port (config webrtc.udp_port), so hosts forward a single port.
package rtc

import (
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/webrtc/v4"
)

// engine holds two webrtc APIs on one UDP mux: a plain one for publisher
// PCs and one with send-side bandwidth estimation for subscriber PCs, so the
// server learns how much each viewer's downlink can take.
type engine struct {
	pubAPI *webrtc.API
	subAPI *webrtc.API

	// The cc interceptor reports each new PC's estimator through a callback
	// that fires synchronously inside NewPeerConnection; subMu serialises
	// creation so the capture is unambiguous.
	subMu   sync.Mutex
	lastEst cc.BandwidthEstimator
}

// newEngine builds the APIs bound to the shared UDP mux. publicIP, when
// non-empty, is advertised as an extra server-reflexive candidate so remote
// (DDNS) clients can reach the SFU through the router.
func newEngine(udpPort int, publicIP string, maxDownlinkKbps int, log *slog.Logger) (*engine, error) {
	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IP{0, 0, 0, 0}, Port: udpPort})
	if err != nil {
		return nil, fmt.Errorf("webrtc udp port %d: %w", udpPort, err)
	}
	log.Info("webrtc media listening", "udp_port", udpPort, "public_ip", publicIP)

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: udpListener}))
	if publicIP != "" {
		se.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeSrflx)
	}
	e := &engine{}

	// Publisher side: stock pion.
	pubME := &webrtc.MediaEngine{}
	if err := pubME.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	pubIR := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(pubME, pubIR); err != nil {
		return nil, err
	}
	e.pubAPI = webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(pubME), webrtc.WithInterceptorRegistry(pubIR))

	// Subscriber side: GCC send-side estimation fed by the viewer's TWCC
	// feedback. Our own layer selection is the shaper, so no pacer.
	if maxDownlinkKbps <= 0 {
		maxDownlinkKbps = 20000
	}
	subME := &webrtc.MediaEngine{}
	if err := subME.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	subIR := &interceptor.Registry{}
	ccf, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(
			gcc.SendSideBWEInitialBitrate(1_000_000),
			gcc.SendSideBWEMinBitrate(150_000),
			gcc.SendSideBWEMaxBitrate(maxDownlinkKbps*1000),
			gcc.SendSideBWEPacer(gcc.NewNoOpPacer()),
		)
	})
	if err != nil {
		return nil, err
	}
	ccf.OnNewPeerConnection(func(_ string, est cc.BandwidthEstimator) {
		e.lastEst = est
	})
	subIR.Add(ccf)
	if err := webrtc.ConfigureTWCCHeaderExtensionSender(subME, subIR); err != nil {
		return nil, err
	}
	if err := webrtc.RegisterDefaultInterceptors(subME, subIR); err != nil {
		return nil, err
	}
	e.subAPI = webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(subME), webrtc.WithInterceptorRegistry(subIR))
	return e, nil
}

// newSubPC creates a subscriber PC and returns its bandwidth estimator.
func (e *engine) newSubPC() (*webrtc.PeerConnection, cc.BandwidthEstimator, error) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	e.lastEst = nil
	pc, err := e.subAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, err
	}
	return pc, e.lastEst, nil
}
