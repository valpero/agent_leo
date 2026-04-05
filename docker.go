package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const dockerSock = "/var/run/docker.sock"

type DockerContainer struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Image  string  `json:"image"`
	Status string  `json:"status"`
	CPUPct float64 `json:"cpu_pct"`
	RAMMb  int     `json:"ram_mb"`
}

// dockerClient makes requests to the Docker Unix socket
var dockerClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", dockerSock, 2*time.Second)
		},
	},
	Timeout: 5 * time.Second,
}

func dockerGet(path string, out interface{}) error {
	resp, err := dockerClient.Get("http://localhost" + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("docker API %s: %s", path, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

type dockerContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
}

type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			Cache uint64 `json:"cache"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

func CollectDocker() ([]DockerContainer, error) {
	var containers []dockerContainer
	if err := dockerGet("/containers/json", &containers); err != nil {
		return nil, err
	}

	var result []DockerContainer
	for _, c := range containers {
		name := c.ID[:12]
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		dc := DockerContainer{
			ID:     c.ID[:12],
			Name:   name,
			Image:  c.Image,
			Status: c.State,
		}

		// Get stats (non-streaming)
		var stats dockerStats
		if err := dockerGet("/containers/"+c.ID+"/stats?stream=false", &stats); err == nil {
			// CPU %
			cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
			sysDelta := float64(stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage)
			numCPU := stats.CPUStats.OnlineCPUs
			if numCPU == 0 {
				numCPU = 1
			}
			if sysDelta > 0 {
				dc.CPUPct = round2(cpuDelta / sysDelta * float64(numCPU) * 100)
			}
			// RAM MB (exclude cache)
			used := stats.MemoryStats.Usage - stats.MemoryStats.Stats.Cache
			dc.RAMMb = int(used / 1024 / 1024)
		}

		result = append(result, dc)
	}
	return result, nil
}
