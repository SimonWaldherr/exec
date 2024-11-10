package exec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "os/exec"

	"golang.org/x/crypto/ssh"
)

// Exec represents a local command execution instance.
type Exec struct {
	ctx    context.Context
	cmd    *Cmd
	stdout io.ReadCloser
	stdin  io.WriteCloser
	stderr io.ReadCloser
}

// Run executes a local command and returns a Exec instance.
func Run(ctx context.Context, name string, args ...string) (*Exec, error) {
	cmd := CommandContext(ctx, name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &Exec{
		ctx:    ctx,
		cmd:    cmd,
		stdout: stdout,
		stdin:  stdin,
		stderr: stderr,
	}, nil
}

// Stdout gibt den stdout-Reader zurück.
func (c *Exec) Stdout() io.Reader {
	return c.stdout
}

// Stdin gibt den stdin-Writer zurück.
func (c *Exec) Stdin() io.Writer {
	return c.stdin
}

// Stderr gibt den stderr-Reader zurück.
func (c *Exec) Stderr() io.Reader {
	return c.stderr
}

// Wait wartet darauf, dass der Befehl beendet wird.
func (c *Exec) Wait() error {
	return c.cmd.Wait()
}

// Close schließt die stdin- und stdout-Streams.
func (c *Exec) Close() error {
	if err := c.stdin.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	if err := c.stdout.Close(); err != nil {
		return fmt.Errorf("failed to close stdout: %w", err)
	}
	if err := c.stderr.Close(); err != nil {
		return fmt.Errorf("failed to close stderr: %w", err)
	}
	return nil
}

// RunAndGetOutput runs a command and returns the combined output (stdout and stderr) as a string.
func RunAndGetOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := CommandContext(ctx, name, args...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: %w, output: %s", err, out.String())
	}
	return out.String(), nil
}

// RunWithChannel runs a command and returns a channel that streams each line of output.
func RunWithChannel(ctx context.Context, name string, args ...string) (<-chan string, error) {
	cmd := CommandContext(ctx, name, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	outputChannel := make(chan string)
	go func() {
		defer close(outputChannel)
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			outputChannel <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			outputChannel <- fmt.Sprintf("error reading output: %v", err)
		}
		cmd.Wait()
	}()

	return outputChannel, nil
}

// Print reads from an io.Reader and prints each line to stdout.
func Print(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	return scanner.Err()
}

// RemoteConnection represents an SSH connection to a remote host.
type RemoteConnection struct {
	client *ssh.Client
}

// SSHConfig contains the details required to establish an SSH connection.
type SSHConfig struct {
	User       string
	Host       string
	Port       int
	Password   string
	PrivateKey []byte
}

// NewRemoteConnection initializes an SSH connection using the given SSHConfig.
func NewRemoteConnection(cfg SSHConfig) (*RemoteConnection, error) {
	authMethods := []ssh.AuthMethod{}
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != nil {
		signer, err := ssh.ParsePrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSH connection: %w", err)
	}

	return &RemoteConnection{client: client}, nil
}

// Run executes a command on the remote server and returns output as a string.
func (rc *RemoteConnection) Run(command string) (string, error) {
	session, err := rc.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var out bytes.Buffer
	session.Stdout = &out
	session.Stderr = &out

	if err := session.Run(command); err != nil {
		return "", fmt.Errorf("remote command failed: %w, output: %s", err, out.String())
	}

	return out.String(), nil
}

// Close closes the SSH connection.
func (rc *RemoteConnection) Close() error {
	return rc.client.Close()
}

// RunRemoteCommand executes a command on a remote server using SSH.
func RunRemoteCommand(ctx context.Context, cfg SSHConfig, command string) (string, error) {
	conn, err := NewRemoteConnection(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to establish SSH connection: %w", err)
	}
	defer conn.Close()

	return conn.Run(command)
}

// RunRemoteCommandWithChannel executes a command on a remote server using SSH and returns a channel that streams each line of output.
func RunRemoteCommandWithChannel(ctx context.Context, cfg SSHConfig, command string) (<-chan string, error) {
	conn, err := NewRemoteConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSH connection: %w", err)
	}
	defer conn.Close()

	return RunWithChannel(ctx, "ssh", fmt.Sprintf("%s@%s", cfg.User, cfg.Host), command)
}

// LsEntry represents a single entry from `ls -l` command output.
type LsEntry struct {
	Permissions string
	Links       int
	Owner       string
	Group       string
	Size        int64
	Modified    time.Time
	Name        string
}

// RunLs parses the output of `ls -l` to return a structured slice of LsEntry.
func RunLs(ctx context.Context, path string) ([]LsEntry, error) {
	output, err := RunAndGetOutput(ctx, "ls", "-l", path)
	if err != nil {
		return nil, err
	}

	var entries []LsEntry
	scanner := bufio.NewScanner(strings.NewReader(output))
	// Skip the header if present
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "total") {
			continue
		}

		// Match format: permissions, links, owner, group, size, date, name
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		// Parse modified time
		modifiedTime, err := time.Parse("Jan 2 15:04", fmt.Sprintf("%s %s %s", fields[5], fields[6], fields[7]))
		if err != nil {
			return nil, fmt.Errorf("failed to parse modification date: %w", err)
		}

		// Parse size and links
		size, _ := strconv.ParseInt(fields[4], 10, 64)
		links, _ := strconv.Atoi(fields[1])

		entry := LsEntry{
			Permissions: fields[0],
			Links:       links,
			Owner:       fields[2],
			Group:       fields[3],
			Size:        size,
			Modified:    modifiedTime,
			Name:        strings.Join(fields[8:], " "),
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// PsEntry represents a single entry from `ps aux` command output.
type PsEntry struct {
	User    string
	PID     int
	CPU     float64
	Memory  float64
	Command string
}

// RunPs parses the output of `ps aux` to return a structured slice of PsEntry.
func RunPs(ctx context.Context) ([]PsEntry, error) {
	output, err := RunAndGetOutput(ctx, "ps", "aux")
	if err != nil {
		return nil, err
	}

	var entries []PsEntry
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Scan() // Skip the header

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		// Parse PID, CPU, and Memory
		pid, _ := strconv.Atoi(fields[1])
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		memory, _ := strconv.ParseFloat(fields[3], 64)

		entry := PsEntry{
			User:    fields[0],
			PID:     pid,
			CPU:     cpu,
			Memory:  memory,
			Command: strings.Join(fields[10:], " "),
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// FindEntry represents a single entry from `find` command output.
type FindEntry struct {
	Path string
}

// RunFind parses the output of `find` command to return a slice of paths.
func RunFind(ctx context.Context, path string, namePattern string) ([]FindEntry, error) {
	output, err := RunAndGetOutput(ctx, "find", path, "-name", namePattern)
	if err != nil {
		return nil, err
	}

	var entries []FindEntry
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		entries = append(entries, FindEntry{Path: scanner.Text()})
	}
	return entries, scanner.Err()
}

// TimeResult represents the output from `time` command.
type TimeResult struct {
	UserTime   time.Duration
	SystemTime time.Duration
	Elapsed    time.Duration
}

// RunTime parses the output of `time` command to return structured timings.
func RunTime(ctx context.Context, command string, args ...string) (*TimeResult, error) {
	// Run `time` with the command
	cmd := CommandContext(ctx, "time", command)
	cmd.Args = append(cmd.Args, args...)

	// Capture stderr (time command outputs to stderr)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	output := stderr.String()
	re := regexp.MustCompile(`user\s+([\d.]+)s\nsys\s+([\d.]+)s\nreal\s+([\d.]+)s`)

	matches := re.FindStringSubmatch(output)
	if len(matches) != 4 {
		return nil, fmt.Errorf("failed to parse time output")
	}

	parseDuration := func(input string) (time.Duration, error) {
		return time.ParseDuration(input + "s")
	}

	userTime, err := parseDuration(matches[1])
	if err != nil {
		return nil, err
	}
	systemTime, err := parseDuration(matches[2])
	if err != nil {
		return nil, err
	}
	elapsed, err := parseDuration(matches[3])
	if err != nil {
		return nil, err
	}

	return &TimeResult{
		UserTime:   userTime,
		SystemTime: systemTime,
		Elapsed:    elapsed,
	}, nil
}

// PingResult represents the output from `ping` command.
type PingResult struct {
	Sequence int
	TimeMs   float64
}

// RunPing parses the output of `ping` to extract sequence and time.
func RunPing(ctx context.Context, host string, count int) ([]PingResult, error) {
	output, err := RunAndGetOutput(ctx, "ping", "-c", strconv.Itoa(count), host)
	if err != nil {
		return nil, err
	}

	var results []PingResult
	scanner := bufio.NewScanner(strings.NewReader(output))
	re := regexp.MustCompile(`icmp_seq=(\d+)\s+time=([\d.]+) ms`)

	for scanner.Scan() {
		line := scanner.Text()
		match := re.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		seq, _ := strconv.Atoi(match[1])
		timeMs, _ := strconv.ParseFloat(match[2], 64)

		results = append(results, PingResult{
			Sequence: seq,
			TimeMs:   timeMs,
		})
	}
	return results, scanner.Err()
}
