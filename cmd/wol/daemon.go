package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"time"
)

const defaultDaemonSocket = "/run/wol-manager/daemon.sock"

type DaemonStatus struct {
	Active     bool      `json:"active"`
	Monitoring bool      `json:"monitoring"`
	Policies   int       `json:"policies"`
	StartedAt  time.Time `json:"started_at,omitempty"`
}

type DaemonStatusArgs struct{}

type daemonStatus = DaemonStatus

type AutomationRuntimeStatus struct {
	Daemon   DaemonStatus       `json:"daemon"`
	Monitors []NUTMonitorStatus `json:"monitors"`
	Cycles   []PowerCycle       `json:"cycles"`
	Policies []restorePlan      `json:"policies"`
	Jobs     []Job              `json:"jobs"`
	Message  string             `json:"message,omitempty"`
}

type daemonRPC struct {
	status      daemonStatus
	jobs        *JobStore
	engine      *Engine
	automations *AutomationManager
}

type daemonClient struct {
	socket string
}

type DaemonRequest struct {
	Action string
	Target string
	Actor  string
	DryRun bool
	Active bool
}

type DaemonResponse struct {
	JSON  []byte
	Code  string
	Error string
}

type daemonCallError struct {
	Code    string
	Message string
}

func (e *daemonCallError) Error() string { return e.Message }

func daemonSocketPath() string {
	if value := os.Getenv("WOL_DAEMON_SOCKET"); value != "" {
		return value
	}
	return defaultDaemonSocket
}

func (d *daemonRPC) Status(_ DaemonStatusArgs, reply *DaemonStatus) error {
	*reply = d.status
	return nil
}

func (d *daemonRPC) Execute(request DaemonRequest, response *DaemonResponse) error {
	var value any
	var err error
	switch request.Action {
	case "wake":
		value, err = d.jobs.createForActor("wake", request.Target, request.Actor, func(ctx context.Context) (any, error) {
			engine := *d.engine
			engine.dry = request.DryRun
			return engine.wakeTree(ctx, request.Target, map[string]bool{})
		})
	case "wake_group":
		value, err = d.jobs.createForActor("wake_group", request.Target, request.Actor, func(ctx context.Context) (any, error) {
			return d.engine.wakeGroup(ctx, request.Target)
		})
	case "wake_all":
		value, err = d.jobs.createForActor("wake_all", "all", request.Actor, func(ctx context.Context) (any, error) {
			return d.engine.wakeAll(ctx)
		})
	case "network_discovery":
		value, err = d.jobs.createForActor("network_discovery", fmt.Sprint(request.Active), request.Actor, func(ctx context.Context) (any, error) {
			if !request.Active {
				return passiveDiscovery(ctx), nil
			}
			timeout := d.engine.cfg.Discovery.Safeguards.Timeout.Value(30 * time.Second)
			limited, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return activeDiscovery(limited, d.engine.cfg, request.DryRun), nil
		})
	case "job_get":
		var found bool
		value, found = d.jobs.get(request.Target)
		if !found {
			response.Code = "not_found"
			err = errors.New("tâche inconnue")
		}
	case "job_cancel":
		err = d.jobs.cancelJob(request.Target)
		if err == nil {
			value = map[string]string{"status": "cancellation_requested"}
		} else {
			response.Code = "not_cancellable"
		}
	case "automation_status":
		value = AutomationRuntimeStatus{
			Daemon:   d.status,
			Monitors: d.automations.monitorStatuses(),
			Cycles:   d.jobs.listCycles(),
			Policies: d.engine.cfg.restorePlans(),
			Jobs:     d.jobs.list(),
		}
	default:
		response.Code = "invalid_action"
		err = fmt.Errorf("action daemon inconnue: %s", request.Action)
	}
	if err != nil {
		if response.Code == "" {
			response.Code = "conflict"
		}
		response.Error = err.Error()
		return nil
	}
	response.JSON, err = json.Marshal(value)
	return err
}

func (c daemonClient) status() (daemonStatus, error) {
	var status daemonStatus
	connection, err := net.DialTimeout("unix", c.socket, 500*time.Millisecond)
	if err != nil {
		return status, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return status, err
	}
	client := rpc.NewClient(connection)
	defer client.Close()
	err = client.Call("Daemon.Status", DaemonStatusArgs{}, &status)
	return status, err
}

func (c daemonClient) execute(request DaemonRequest, value any) error {
	connection, err := net.DialTimeout("unix", c.socket, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	client := rpc.NewClient(connection)
	defer client.Close()
	var response DaemonResponse
	if err := client.Call("Daemon.Execute", request, &response); err != nil {
		return err
	}
	if response.Error != "" {
		return &daemonCallError{Code: response.Code, Message: response.Error}
	}
	return json.Unmarshal(response.JSON, value)
}

func (c daemonClient) createWake(target, actor string, dryRun bool) (Job, error) {
	var job Job
	err := c.execute(DaemonRequest{Action: "wake", Target: target, Actor: actor, DryRun: dryRun}, &job)
	return job, err
}

func (c daemonClient) createGroupWake(target, actor string) (Job, error) {
	var job Job
	err := c.execute(DaemonRequest{Action: "wake_group", Target: target, Actor: actor}, &job)
	return job, err
}

func (c daemonClient) createWakeAll(actor string) (Job, error) {
	var job Job
	err := c.execute(DaemonRequest{Action: "wake_all", Actor: actor}, &job)
	return job, err
}

func (c daemonClient) createDiscovery(actor string, active, dryRun bool) (Job, error) {
	var job Job
	err := c.execute(DaemonRequest{Action: "network_discovery", Actor: actor, Active: active, DryRun: dryRun}, &job)
	return job, err
}

func (c daemonClient) job(id string) (Job, error) {
	var job Job
	err := c.execute(DaemonRequest{Action: "job_get", Target: id}, &job)
	return job, err
}

func (c daemonClient) cancelJob(id string) error {
	var response map[string]string
	return c.execute(DaemonRequest{Action: "job_cancel", Target: id}, &response)
}

func (c daemonClient) automationStatus() (AutomationRuntimeStatus, error) {
	var status AutomationRuntimeStatus
	err := c.execute(DaemonRequest{Action: "automation_status"}, &status)
	return status, err
}

func daemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var networkError *net.OpError
	return errors.As(err, &networkError) || errors.Is(err, rpc.ErrShutdown) || os.IsNotExist(err)
}

func serveDaemon(ctx context.Context, cfg *Config, jobs *JobStore, engine *Engine, socket string) error {
	listener, err := listenDaemonSocket(socket)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socket)
	}()
	policyCount := len(cfg.restorePlans())
	if engine.eventLog == nil {
		engine.eventLog = processServiceLog
	}
	engine.logEvent("INFO", "daemon: démarrage; socket=%s politiques=%d", socket, policyCount)

	if err := jobs.markInterrupted(); err != nil {
		return fmt.Errorf("revalidation des tâches interrompues: %w", err)
	}
	manager := newAutomationManager(cfg, jobs, engine)
	resumeJobs(jobs, engine, manager)

	service := rpc.NewServer()
	if err := service.RegisterName("Daemon", &daemonRPC{status: daemonStatus{
		Active:     true,
		Monitoring: policyCount > 0,
		Policies:   policyCount,
		StartedAt:  time.Now().UTC(),
	}, jobs: jobs, engine: engine, automations: manager}); err != nil {
		return err
	}
	if policyCount > 0 {
		engine.logEvent("INFO", "daemon: moniteur NUT actif; politiques=%d", policyCount)
		go manager.monitor(ctx)
	} else {
		engine.logEvent("INFO", "daemon: moniteur NUT inactif; aucune politique configurée")
	}

	acceptErrors := make(chan error, 1)
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				acceptErrors <- acceptErr
				return
			}
			go service.ServeConn(connection)
		}
	}()

	select {
	case <-ctx.Done():
		engine.logEvent("INFO", "daemon: arrêt demandé")
		_ = listener.Close()
		return nil
	case err := <-acceptErrors:
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			engine.logEvent("INFO", "daemon: arrêt demandé")
			return nil
		}
		engine.logEvent("ERROR", "daemon: arrêt inattendu: %v", err)
		return err
	}
}

func listenDaemonSocket(socket string) (net.Listener, error) {
	directory := filepath.Dir(socket)
	if err := os.MkdirAll(directory, 0750); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket daemon non sûr: %s", socket)
		}
		connection, dialErr := net.DialTimeout("unix", socket, 200*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return nil, fmt.Errorf("un daemon WoL Manager est déjà actif")
		}
		if err := os.Remove(socket); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socket)
		return nil, err
	}
	return listener, nil
}
