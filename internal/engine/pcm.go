package engine

import (
	"github.com/at-wat/ebml-go"
	"github.com/at-wat/ebml-go/webm"
	"io"
	"math"
)

// Matroska PCM keeps each decoded frame's timestamp, including gaps. Raw PCM
// cannot express these gaps. ebml-go handles the container's binary encoding.
func pcmHeader(w io.Writer, rate int) error {
	type audio struct {
		SamplingFrequency float64 `ebml:"SamplingFrequency"`
		Channels          uint64  `ebml:"Channels"`
		BitDepth          uint64  `ebml:"BitDepth"`
	}
	type track struct {
		TrackNumber uint64 `ebml:"TrackNumber"`
		TrackUID    uint64 `ebml:"TrackUID"`
		TrackType   uint64 `ebml:"TrackType"`
		CodecID     string `ebml:"CodecID"`
		Audio       audio  `ebml:"Audio"`
	}
	var header struct {
		Header  webm.EBMLHeader `ebml:"EBML"`
		Segment struct {
			Info   webm.Info `ebml:"Info"`
			Tracks struct {
				Entries []track `ebml:"TrackEntry"`
			} `ebml:"Tracks"`
		} `ebml:"Segment,size=unknown"`
	}
	header.Header = *webm.DefaultEBMLHeader
	header.Header.DocType = "matroska"
	header.Segment.Info = webm.Info{TimecodeScale: 1000, MuxingApp: "szjm-go/ebml-go", WritingApp: "szjm-go"}
	header.Segment.Tracks.Entries = []track{{1, 1, 2, "A_PCM/FLOAT/IEEE", audio{float64(rate), 2, 32}}}
	return ebml.Marshal(&header, w)
}
func pcmFrame(w io.Writer, seconds float64, b []byte) error {
	var v struct {
		Cluster webm.Cluster `ebml:"Cluster"`
	}
	v.Cluster.Timecode = uint64(math.Round(seconds * 1e6))
	v.Cluster.SimpleBlock = []ebml.Block{{TrackNumber: 1, Keyframe: true, Data: [][]byte{b}}}
	return ebml.Marshal(&v, w)
}
