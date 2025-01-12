package videoserver

import (
	"log"
	"strings"

	"github.com/gin-contrib/cors"

	"github.com/deepch/vdk/av"
	"github.com/google/uuid"
)

// Application is a configuration parameters for application
type Application struct {
	Server     *ServerInfo     `json:"server"`
	Streams    *StreamsStorage `json:"streams"`
	HLS        HLSInfo         `json:"hls"`
	CorsConfig *cors.Config    `json:"-"`
}

// HLSInfo is an information about HLS parameters for server
type HLSInfo struct {
	MsPerSegment int64  `json:"hls_ms_per_segment"`
	Directory    string `json:"-"`
	WindowSize   uint   `json:"hls_window_size"`
	Capacity     uint   `json:"hls_window_capacity"`
}

// ServerInfo is an information about server
type ServerInfo struct {
	HTTPAddr      string `json:"http_addr"`
	VideoHTTPPort int    `json:"http_port"`
	APIHTTPPort   int    `json:"-"`
}

// NewApplication Prepare configuration for application
func NewApplication(cfg *ConfigurationArgs) (*Application, error) {
	tmp := Application{
		Server: &ServerInfo{
			HTTPAddr:      cfg.Server.HTTPAddr,
			VideoHTTPPort: cfg.Server.VideoHTTPPort,
			APIHTTPPort:   cfg.Server.APIHTTPPort,
		},
		Streams: NewStreamsStorageDefault(),
		HLS: HLSInfo{
			MsPerSegment: cfg.HLSConfig.MsPerSegment,
			Directory:    cfg.HLSConfig.Directory,
			WindowSize:   cfg.HLSConfig.WindowSize,
			Capacity:     cfg.HLSConfig.Capacity,
		},
	}
	if cfg.CorsConfig.UseCORS {
		tmp.setCors(&cfg.CorsConfig)
	}
	for _, streamCfg := range cfg.Streams {
		validUUID, err := uuid.Parse(streamCfg.GUID)
		if err != nil {
			log.Printf("Not valid UUID: %s\n", streamCfg.GUID)
			continue
		}
		tmp.Streams.Streams[validUUID] = NewStreamConfiguration(streamCfg.URL, streamCfg.StreamTypes)
		verbose := strings.ToLower(streamCfg.Verbose)
		if verbose == "v" {
			tmp.Streams.Streams[validUUID].verbose = true
		} else if verbose == "vvv" {
			tmp.Streams.Streams[validUUID].verbose = true
			tmp.Streams.Streams[validUUID].verboseDetailed = true
		}
	}
	return &tmp, nil
}

func (app *Application) setCors(cfg *CorsConfiguration) {
	newCors := cors.DefaultConfig()
	app.CorsConfig = &newCors
	app.CorsConfig.AllowOrigins = cfg.AllowOrigins
	if len(cfg.AllowMethods) != 0 {
		app.CorsConfig.AllowMethods = cfg.AllowMethods
	}
	if len(cfg.AllowHeaders) != 0 {
		app.CorsConfig.AllowHeaders = cfg.AllowHeaders
	}
	app.CorsConfig.ExposeHeaders = cfg.ExposeHeaders
	app.CorsConfig.AllowCredentials = cfg.AllowCredentials
}

func (app *Application) cast(streamID uuid.UUID, pck av.Packet, hlsEnabled bool) error {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	curStream, ok := app.Streams.Streams[streamID]
	if !ok {
		return ErrStreamNotFound
	}
	if hlsEnabled {
		curStream.hlsChanel <- pck
	}
	for _, v := range curStream.Clients {
		if len(v.c) < cap(v.c) {
			v.c <- pck
		}
	}
	return nil
}
func (app *Application) exists(streamID uuid.UUID) bool {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	_, ok := app.Streams.Streams[streamID]
	return ok
}

func (app *Application) existsWithType(streamID uuid.UUID, streamType string) bool {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	stream, ok := app.Streams.Streams[streamID]
	if !ok {
		return false
	}
	supportedTypes := stream.SupportedStreamTypes
	typeEnabled := typeExists(streamType, supportedTypes)
	return ok && typeEnabled
}

func (app *Application) addCodec(streamID uuid.UUID, codecs []av.CodecData) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	app.Streams.Streams[streamID].Codecs = codecs
}

func (app *Application) getCodec(streamID uuid.UUID) ([]av.CodecData, error) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	curStream, ok := app.Streams.Streams[streamID]
	if !ok {
		return nil, ErrStreamNotFound
	}
	return curStream.Codecs, nil
}

func (app *Application) updateStreamStatus(streamID uuid.UUID, status bool) error {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	t, ok := app.Streams.Streams[streamID]
	if !ok {
		return ErrStreamNotFound
	}
	t.Status = status
	app.Streams.Streams[streamID] = t
	return nil
}

func (app *Application) clientAdd(streamID uuid.UUID) (uuid.UUID, chan av.Packet, error) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	clientID, err := uuid.NewUUID()
	if err != nil {
		return uuid.UUID{}, nil, err
	}
	ch := make(chan av.Packet, 100)
	curStream, ok := app.Streams.Streams[streamID]
	if !ok {
		return uuid.UUID{}, nil, ErrStreamNotFound
	}
	curStream.Clients[clientID] = viewer{c: ch}
	return clientID, ch, nil
}

func (app *Application) clientDelete(streamID, clientID uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	delete(app.Streams.Streams[streamID].Clients, clientID)
}

func (app *Application) startHlsCast(streamID uuid.UUID, stopCast chan bool) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	go app.startHls(streamID, app.Streams.Streams[streamID].hlsChanel, stopCast)
}

func (app *Application) list() (uuid.UUID, []uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	res := []uuid.UUID{}
	first := uuid.UUID{}
	for k := range app.Streams.Streams {
		if first.String() == "" {
			first = k
		}
		res = append(res, k)
	}
	return first, res
}
