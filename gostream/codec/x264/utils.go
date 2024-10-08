package x264

import (
	"go.viam.com/rdk/gostream"
	"go.viam.com/rdk/gostream/codec"
	"go.viam.com/rdk/logging"
)

// DefaultStreamConfig configures x264 as the encoder for a stream.
var DefaultStreamConfig gostream.StreamConfig

func init() {
	DefaultStreamConfig.VideoEncoderFactory = NewEncoderFactory()
}

// NewEncoderFactory returns an x264 encoder factory.
func NewEncoderFactory() codec.VideoEncoderFactory {
	return &factory{}
}

type factory struct{}

func (f *factory) New(width, height, keyFrameInterval int, logger logging.Logger) (codec.VideoEncoder, error) {
	return NewEncoder(width, height, keyFrameInterval, logger)
}

func (f *factory) MIMEType() string {
	return "video/H264"
}
