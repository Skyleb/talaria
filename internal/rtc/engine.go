// Package rtc implements Phase 2: the embedded SFU for voice/video/screen share.
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

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// newEngine builds a webrtc.API bound to the shared UDP mux.
// publicIP, when non-empty, is advertised as an extra server-reflexive
// candidate so remote (DDNS) clients can reach the SFU through the router.
func newEngine(udpPort int, publicIP string, log *slog.Logger) (*webrtc.API, error) {
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

	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(me, ir); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(me),
		webrtc.WithInterceptorRegistry(ir),
	), nil
}
