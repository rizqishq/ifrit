package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

type PortEntry struct {
	PID         int32  `json:"pid"`
	LocalAddr   string `json:"local_addr"`
	LocalPort   uint32 `json:"local_port"`
	RemoteAddr  string `json:"remote_addr"`
	RemotePort  uint32 `json:"remote_port"`
	Proto       string `json:"proto"`
	Status      string `json:"status"`
	ProcessName string `json:"process_name"`
	User        string `json:"user"`
}

type ScanResult struct {
	Port int
	Open bool
}

func getConnections(protoFilter, stateFilter string, portFilter uint32) ([]PortEntry, error) {
	kind := "inet"
	if protoFilter == "tcp" {
		kind = "tcp"
	} else if protoFilter == "udp" {
		kind = "udp"
	}

	conns, err := psnet.Connections(kind)
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}

	var entries []PortEntry
	for _, c := range conns {
		if c.Laddr.Port == 0 {
			continue
		}
		if portFilter > 0 && c.Laddr.Port != portFilter {
			continue
		}
		if stateFilter != "" && !strings.EqualFold(c.Status, stateFilter) {
			continue
		}

		proto := "TCP"
		if c.Type == 2 {
			proto = "UDP"
		}

		entry := PortEntry{
			PID:        c.Pid,
			LocalAddr:  c.Laddr.IP,
			LocalPort:  c.Laddr.Port,
			RemoteAddr: c.Raddr.IP,
			RemotePort: c.Raddr.Port,
			Proto:      proto,
			Status:     c.Status,
		}

		if c.Pid > 0 {
			if p, err := process.NewProcess(c.Pid); err == nil {
				entry.ProcessName, _ = p.Name()
				entry.User, _ = p.Username()
			}
		}

		if entry.ProcessName == "" {
			entry.ProcessName = "-"
		}
		if entry.User == "" {
			entry.User = "-"
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LocalPort < entries[j].LocalPort
	})

	return entries, nil
}

func killProcess(pid int32, force bool) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	name, _ := p.Name()

	if force {
		if err := p.Kill(); err != nil {
			return fmt.Errorf("failed to kill process %d (%s): %w", pid, name, err)
		}
		fmt.Printf("Killed process %d (%s) with SIGKILL\n", pid, name)
	} else {
		if err := p.Terminate(); err != nil {
			return fmt.Errorf("failed to terminate process %d (%s): %w", pid, name, err)
		}
		fmt.Printf("Terminated process %d (%s) with SIGTERM\n", pid, name)
	}

	return nil
}

// scanPorts probes a range of TCP ports concurrently, bounded by a semaphore
// to avoid exhausting file descriptors.
func scanPorts(host string, startPort, endPort int, timeout time.Duration) []ScanResult {
	var (
		results []ScanResult
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	sem := make(chan struct{}, 100)

	for port := startPort; port <= endPort; port++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }()

			addr := fmt.Sprintf("%s:%d", host, p)
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err == nil {
				conn.Close()
				mu.Lock()
				results = append(results, ScanResult{Port: p, Open: true})
				mu.Unlock()
			}
		}(port)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Port < results[j].Port
	})

	return results
}

func printTable(entries []PortEntry) {
	if len(entries) == 0 {
		fmt.Println("No connections found.")
		return
	}

	fmt.Printf("%-8s %-7s %-7s %-14s %-20s %-12s\n",
		"PID", "PORT", "PROTO", "STATUS", "PROCESS", "USER")
	fmt.Println(strings.Repeat("─", 72))

	for _, e := range entries {
		fmt.Printf("%-8d %-7d %-7s %-14s %-20s %-12s\n",
			e.PID, e.LocalPort, e.Proto, e.Status, e.ProcessName, e.User)
	}

	fmt.Printf("\nTotal: %d connections\n", len(entries))
}

func printJSON(entries []PortEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printCSV(entries []PortEntry) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	w.Write([]string{"pid", "port", "proto", "status", "process", "user"})
	for _, e := range entries {
		w.Write([]string{
			strconv.Itoa(int(e.PID)),
			strconv.Itoa(int(e.LocalPort)),
			e.Proto,
			e.Status,
			e.ProcessName,
			e.User,
		})
	}
	return nil
}

var rootCmd = &cobra.Command{
	Use:   "ifrit",
	Short: "A port & process monitor for developers",
	Long: `ifrit is a CLI tool for monitoring network ports, viewing active
connections, and managing processes. Built for developers who are
tired of deciphering lsof and netstat output.`,
}

var listCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all active network connections",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		proto, _ := cmd.Flags().GetString("proto")
		state, _ := cmd.Flags().GetString("state")
		port, _ := cmd.Flags().GetUint32("port")
		format, _ := cmd.Flags().GetString("format")

		entries, err := getConnections(proto, state, port)
		if err != nil {
			return err
		}

		switch format {
		case "json":
			return printJSON(entries)
		case "csv":
			return printCSV(entries)
		default:
			printTable(entries)
		}

		return nil
	},
}

var killCmd = &cobra.Command{
	Use:   "kill <pid>",
	Short: "Kill a process by PID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid PID: %s", args[0])
		}

		force, _ := cmd.Flags().GetBool("force")
		return killProcess(int32(pid), force)
	},
}

var scanCmd = &cobra.Command{
	Use:   "scan <host>",
	Short: "Scan open ports on a host",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host := args[0]
		ports, _ := cmd.Flags().GetString("ports")
		timeout, _ := cmd.Flags().GetInt("timeout")

		startPort, endPort := 1, 1024
		if ports != "" {
			parts := strings.Split(ports, "-")
			if len(parts) == 2 {
				startPort, _ = strconv.Atoi(parts[0])
				endPort, _ = strconv.Atoi(parts[1])
			} else if len(parts) == 1 {
				startPort, _ = strconv.Atoi(parts[0])
				endPort = startPort
			}
		}

		fmt.Printf("Scanning %s (ports %d-%d)...\n\n", host, startPort, endPort)

		results := scanPorts(host, startPort, endPort, time.Duration(timeout)*time.Millisecond)

		if len(results) == 0 {
			fmt.Println("No open ports found.")
			return nil
		}

		fmt.Printf("%-8s %-10s\n", "PORT", "STATE")
		fmt.Println(strings.Repeat("─", 20))
		for _, r := range results {
			fmt.Printf("%-8d %-10s\n", r.Port, "open")
		}
		fmt.Printf("\n%d open ports found.\n", len(results))

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of ifrit",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ifrit v%s\n", version)
	},
}

func init() {
	listCmd.Flags().StringP("proto", "p", "all", "protocol filter (tcp|udp|all)")
	listCmd.Flags().StringP("state", "s", "", "filter by state (LISTEN|ESTABLISHED|...)")
	listCmd.Flags().Uint32("port", 0, "filter by specific port number")
	listCmd.Flags().StringP("format", "f", "table", "output format (table|json|csv)")

	killCmd.Flags().BoolP("force", "F", false, "force kill with SIGKILL instead of SIGTERM")

	scanCmd.Flags().String("ports", "1-1024", "port range to scan (e.g. 80-443)")
	scanCmd.Flags().Int("timeout", 500, "connection timeout in milliseconds")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
