package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Server struct {
	Id                       int
	MaximumActiveConnections int64
	CurrentActiveConnections int64
	Status                   atomic.Bool
	URL                      string
	ReverseProxy             *httputil.ReverseProxy
}

type LoadBalancer struct {
	Servers                []Server
	TotalActiveConnections int64
	TotalRequests          int64
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&lb.TotalRequests, 1)

	server := lb.selectServer()
	if server == nil {
		http.Error(w, "No available servers", http.StatusServiceUnavailable)
		return
	}

	atomic.AddInt64(&server.CurrentActiveConnections, 1)
	atomic.AddInt64(&lb.TotalActiveConnections, 1)

	defer func() {
		atomic.AddInt64(&server.CurrentActiveConnections, -1)
		atomic.AddInt64(&lb.TotalActiveConnections, -1)
	}()

	server.ReverseProxy.ServeHTTP(w, r)
}

func (lb *LoadBalancer) selectServer() *Server {
	var optimalServer *Server
	for i := 0; i < len(lb.Servers); i++ {
		currentLoad := float64(atomic.LoadInt64(&lb.Servers[i].CurrentActiveConnections)) / float64(lb.Servers[i].MaximumActiveConnections)
		if !lb.Servers[i].Status.Load() || currentLoad >= 1.0 {
			continue
		}
		if optimalServer == nil {
			optimalServer = &lb.Servers[i]
			continue
		}
		optimalLoad := float64(atomic.LoadInt64(&optimalServer.CurrentActiveConnections)) / float64(optimalServer.MaximumActiveConnections)
		if currentLoad < optimalLoad {
			optimalServer = &lb.Servers[i]
		}
	}
	return optimalServer
}

func setupReversProxies(lb *LoadBalancer) {
	for i := range lb.Servers {
		serverURL, _ := url.Parse(lb.Servers[i].URL)
		lb.Servers[i].ReverseProxy = httputil.NewSingleHostReverseProxy(serverURL)
	}
}

func healthCheck(lb *LoadBalancer, pause time.Duration) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(pause)
	defer ticker.Stop()

	for {
		var wg sync.WaitGroup
		for i := range lb.Servers {
			wg.Add(1)
			go func(serv *Server) {
				defer wg.Done()
				resp, err := client.Get(serv.URL + "/health")

				newStatus := false
				if err == nil && resp.StatusCode == 200 {
					newStatus = true
				}
				if resp != nil {
					resp.Body.Close()
				}

				serv.Status.Store(newStatus)
			}(&lb.Servers[i])
		}
		wg.Wait()
		<-ticker.C
	}
}

type tickMsg time.Time

type model struct {
	lb           *LoadBalancer
	table        table.Model
	lastRequests int64
	lastTick     time.Time
	rps          float64
}

func newModel(lb *LoadBalancer) model {
	columns := []table.Column{
		{Title: "ID", Width: 4},
		{Title: "URL", Width: 25},
		{Title: "Status", Width: 8},
		{Title: "Conn", Width: 6},
		{Title: "Max", Width: 6},
		{Title: "Load %", Width: 8},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(false),
		table.WithHeight(len(lb.Servers)+1),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#6272a4")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#8be9fd"))
	s.Selected = lipgloss.NewStyle()
	t.SetStyles(s)

	return model{
		lb:       lb,
		table:    t,
		lastTick: time.Now(),
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tickMsg:
		now := time.Time(msg)
		elapsed := now.Sub(m.lastTick).Seconds()
		currentRequests := atomic.LoadInt64(&m.lb.TotalRequests)
		if elapsed > 0 {
			m.rps = float64(currentRequests-m.lastRequests) / elapsed
		}
		m.lastRequests = currentRequests
		m.lastTick = now

		rows := []table.Row{}
		for i := range m.lb.Servers {
			srv := &m.lb.Servers[i]
			status := "DOWN"
			if srv.Status.Load() {
				status = "UP"
			}
			conn := atomic.LoadInt64(&srv.CurrentActiveConnections)
			load := float64(conn) / float64(srv.MaximumActiveConnections) * 100

			rows = append(rows, table.Row{
				fmt.Sprintf("%d", srv.Id),
				srv.URL,
				status,
				fmt.Sprintf("%d", conn),
				fmt.Sprintf("%d", srv.MaximumActiveConnections),
				fmt.Sprintf("%.1f%%", load),
			})
		}
		m.table.SetRows(rows)
		return m, tickCmd()
	}
	return m, nil
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#8be9fd")).
		MarginBottom(1)

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8be9fd")).
		MarginBottom(1)

	title := titleStyle.Render(" Load Balancer")
	stats := statsStyle.Render(fmt.Sprintf(
		"Active Connections: %d  |  Total Requests: %d  |  RPS: %.1f",
		atomic.LoadInt64(&m.lb.TotalActiveConnections),
		atomic.LoadInt64(&m.lb.TotalRequests),
		m.rps,
	))
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4")).Render("\nPress 'q' to quit")

	return fmt.Sprintf("%s\n%s\n%s%s", title, stats, m.table.View(), help)
}

func main() {
	log.SetOutput(io.Discard)
	filepath := flag.String("servers", "", "path to a file with servers info")
	healthPauseNum := flag.Int("health-timeout", 120, "Pause in seconds between running health check on servers")
	flag.Parse()

	healthPause := time.Duration(*healthPauseNum) * time.Second

	serversInfoFile, err := os.ReadFile(*filepath)
	if err != nil {
		fmt.Print(err)
		return
	}

	var lb LoadBalancer
	err = json.Unmarshal(serversInfoFile, &lb)
	if err != nil {
		fmt.Print(err)
		return
	}

	setupReversProxies(&lb)

	go healthCheck(&lb, healthPause)

	go func() {
		err = http.ListenAndServeTLS(":8443", "creds/cert.pem", "creds/key.pem", &lb)
		if err != nil {
			fmt.Println("Error starting server", err)
			os.Exit(1)
		}
	}()

	p := tea.NewProgram(newModel(&lb))
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running TUI:", err)
		os.Exit(1)
	}
}
