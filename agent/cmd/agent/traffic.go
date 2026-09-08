package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coxpanel/shared/contract"
)

type trafficSample struct {
	InboundID int64 `json:"inboundId"`
	UserID    int64 `json:"userId,omitempty"`
	UpBytes   int64 `json:"upBytes,string"`
	DownBytes int64 `json:"downBytes,string"`
}
type trafficReport struct {
	SchemaVersion string          `json:"schemaVersion"`
	NodeID        int64           `json:"nodeId"`
	EpochID       string          `json:"epochId"`
	Sequence      int64           `json:"sequence"`
	PeriodStart   time.Time       `json:"periodStart"`
	PeriodEnd     time.Time       `json:"periodEnd"`
	Complete      bool            `json:"complete"`
	Samples       []trafficSample `json:"samples"`
}
type trafficDisk struct {
	NodeID   int64            `json:"nodeId"`
	Epoch    string           `json:"epoch"`
	Sequence int64            `json:"sequence"`
	LastAt   time.Time        `json:"lastAt"`
	Uptime   int64            `json:"uptime"`
	Counters map[string]int64 `json:"counters"`
	Pending  *trafficReport   `json:"pending,omitempty"`
}

var trafficMutex sync.Mutex

func collectTraffic() {
	address := os.Getenv("COXPANEL_STATS_ADDR")
	if address == "" {
		return
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return
	}
	trafficMutex.Lock()
	defer trafficMutex.Unlock()
	path := *cfgPath + ".traffic-state.json"
	state := trafficDisk{NodeID: *nodeID, Counters: map[string]int64{}}
	if content, readErr := os.ReadFile(path); readErr == nil {
		if len(content) > 4<<20 || json.Unmarshal(content, &state) != nil || state.NodeID != *nodeID {
			return
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return
	}
	if state.Pending != nil {
		if !sendTrafficReport(*state.Pending) {
			return
		}
		state.Pending = nil
		if saveTrafficState(path, state) != nil {
			return
		}
	}
	counters, uptime, err := readStats(address)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	reset := state.Epoch == "" || uptime < state.Uptime || (!state.LastAt.IsZero() && uptime-state.Uptime < int64(now.Sub(state.LastAt).Seconds())-5)
	for key, value := range counters {
		if value < state.Counters[key] {
			reset = true
		}
	}
	if reset {
		raw := make([]byte, 16)
		if _, err = rand.Read(raw); err != nil {
			return
		}
		raw[6] = raw[6]&15 | 64
		raw[8] = raw[8]&63 | 128
		state.Epoch = fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
		state.Sequence = 0
		state.LastAt = now.Add(-time.Second)
	}
	samples, err := statsSamples(counters)
	if err != nil {
		return
	}
	state.Sequence++
	report := trafficReport{SchemaVersion: "traffic/v2", NodeID: *nodeID, EpochID: state.Epoch, Sequence: state.Sequence, PeriodStart: state.LastAt, PeriodEnd: now, Complete: !reset, Samples: samples}
	state.LastAt = now
	state.Uptime = uptime
	state.Counters = counters
	state.Pending = &report
	if saveTrafficState(path, state) != nil {
		return
	}
	if sendTrafficReport(report) {
		state.Pending = nil
		_ = saveTrafficState(path, state)
	}
}

func saveTrafficState(path string, state trafficDisk) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	content, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
func sendTrafficReport(report trafficReport) bool {
	body, err := json.Marshal(report)
	if err != nil {
		return false
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(*panelURL, "/")+"/api/agent/report-traffic", bytes.NewReader(body))
	if err != nil {
		return false
	}
	request.Header.Set(contract.AgentNodeIDHeader, fmt.Sprint(*nodeID))
	request.Header.Set(contract.AgentCredentialHeader, agentCredential())
	request.Header.Set("Content-Type", "application/json")
	response, err := agentHTTPClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var receipt struct {
		Accepted bool  `json:"accepted"`
		Sequence int64 `json:"acceptedSequence"`
	}
	return response.StatusCode == 200 && json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&receipt) == nil && receipt.Accepted && receipt.Sequence == report.Sequence
}

func statsSamples(counters map[string]int64) ([]trafficSample, error) {
	series := map[string]*trafficSample{}
	for name, value := range counters {
		if value < 0 {
			return nil, errors.New("counter_negative")
		}
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[2] != "traffic" || (parts[3] != "uplink" && parts[3] != "downlink") {
			continue
		}
		var inboundID, userID int64
		var err error
		switch parts[0] {
		case "inbound":
			segments := strings.Split(parts[1], "-")
			if len(segments) < 2 || segments[0] != "in" {
				continue
			}
			inboundID, err = strconv.ParseInt(segments[1], 10, 64)
		case "user":
			segments := strings.Split(parts[1], "-")
			if len(segments) != 4 || segments[0] != "u" || segments[2] != "in" {
				continue
			}
			userID, err = strconv.ParseInt(segments[1], 10, 64)
			if err == nil {
				inboundID, err = strconv.ParseInt(segments[3], 10, 64)
			}
		default:
			continue
		}
		if err != nil || inboundID <= 0 || userID < 0 {
			return nil, errors.New("counter_identity_invalid")
		}
		key := fmt.Sprintf("%d:%d", inboundID, userID)
		sample := series[key]
		if sample == nil {
			sample = &trafficSample{InboundID: inboundID, UserID: userID}
			series[key] = sample
		}
		if parts[3] == "uplink" {
			sample.UpBytes = value
		} else {
			sample.DownBytes = value
		}
	}
	result := make([]trafficSample, 0, len(series))
	for _, sample := range series {
		result = append(result, *sample)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].InboundID != result[right].InboundID {
			return result[left].InboundID < result[right].InboundID
		}
		return result[left].UserID < result[right].UserID
	})
	return result, nil
}

func readStats(address string) (map[string]int64, int64, error) {
	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	query := func(method string) ([]byte, error) {
		request, err := http.NewRequest(http.MethodPost, "http://"+address+"/v2ray.core.app.stats.command.StatsService/"+method, bytes.NewReader([]byte{0, 0, 0, 0, 0}))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/grpc")
		request.Header.Set("TE", "trailers")
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		grpcStatus := response.Trailer.Get("Grpc-Status")
		if response.StatusCode != 200 || (grpcStatus != "" && grpcStatus != "0") || len(body) < 5 || body[0] != 0 || int(binary.BigEndian.Uint32(body[1:5])) != len(body)-5 {
			return nil, errors.New("stats_unavailable")
		}
		return body[5:], nil
	}
	body, err := query("QueryStats")
	if err != nil {
		return nil, 0, err
	}
	fields, err := protobufFields(body)
	if err != nil {
		return nil, 0, err
	}
	counters := map[string]int64{}
	for _, field := range fields {
		if field.Number != 1 || field.Wire != 2 {
			continue
		}
		stats, decodeErr := protobufFields(field.Data)
		if decodeErr != nil {
			return nil, 0, decodeErr
		}
		var name string
		var value uint64
		for _, stat := range stats {
			if stat.Number == 1 && stat.Wire == 2 {
				name = string(stat.Data)
			}
			if stat.Number == 2 && stat.Wire == 0 {
				value = stat.Value
			}
		}
		if value > math.MaxInt64 {
			return nil, 0, errors.New("stats_overflow")
		}
		if name != "" {
			counters[name] = int64(value)
		}
	}
	body, err = query("GetSysStats")
	if err != nil {
		return nil, 0, err
	}
	fields, err = protobufFields(body)
	if err != nil {
		return nil, 0, err
	}
	var uptime int64
	for _, field := range fields {
		if field.Number == 10 && field.Wire == 0 {
			uptime = int64(field.Value)
		}
	}
	return counters, uptime, nil
}

type protobufField struct {
	Number uint64
	Wire   uint64
	Value  uint64
	Data   []byte
}

func protobufFields(body []byte) ([]protobufField, error) {
	result := []protobufField{}
	for len(body) > 0 {
		tag, count := binary.Uvarint(body)
		if count <= 0 {
			return nil, errors.New("stats_protobuf_invalid")
		}
		body = body[count:]
		field := protobufField{Number: tag >> 3, Wire: tag & 7}
		switch field.Wire {
		case 0:
			value, size := binary.Uvarint(body)
			if size <= 0 {
				return nil, errors.New("stats_protobuf_invalid")
			}
			field.Value = value
			body = body[size:]
		case 2:
			size, count := binary.Uvarint(body)
			if count <= 0 || size > uint64(len(body)-count) {
				return nil, errors.New("stats_protobuf_invalid")
			}
			body = body[count:]
			field.Data = body[:int(size)]
			body = body[int(size):]
		case 1:
			if len(body) < 8 {
				return nil, errors.New("stats_protobuf_invalid")
			}
			body = body[8:]
		case 5:
			if len(body) < 4 {
				return nil, errors.New("stats_protobuf_invalid")
			}
			body = body[4:]
		default:
			return nil, errors.New("stats_protobuf_invalid")
		}
		result = append(result, field)
	}
	return result, nil
}
