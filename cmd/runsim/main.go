package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// token ID used to retrieve values from secure parameter storage
	ghAppTokenID    = "github-sim-app-key"
	slackAppTokenID = "slack-app-key"

	logBucketPrefix = "sim-logs-"
	defaultTimeout  = 24 * time.Hour
)

var (
	// default seeds
	seeds = []int{
		1, 2, 4, 7, 32, 123, 124, 582, 1893, 2989,
		3012, 4728, 37827, 981928, 87821, 891823782,
		989182, 89182391, 11, 22, 44, 77, 99, 2020,
		3232, 123123, 124124, 582582, 18931893,
		29892989, 30123012, 47284728, 7601778, 8090485,
		977367484, 491163361, 424254581, 673398983,
	}

	// goroutine-safe process map
	procs map[int]*os.Process
	mutex *sync.Mutex

	// command line arguments and options
	jobs     = runtime.GOMAXPROCS(0)
	testname string

	// log stuff
	runsimLogFile *os.File
	timeout       time.Duration
)

type Seed struct {
	Num          int
	Stdout       string
	Stderr       string
	ExportParams string
	ExportState  string
	Failed       bool
}

func init() {
	log.SetPrefix("")
	log.SetFlags(0)

	initFlags()

	procs = map[int]*os.Process{}
	mutex = &sync.Mutex{}
}

func main() {
	tempDir, err := ioutil.TempDir("", "sim-logs-")
	if err != nil {
		log.Fatalf("ERROR: ioutil.TempDir: %v", err)
	}

	okZip = filepath.Join(tempDir, "ok.zip")
	failedZip = filepath.Join(tempDir, "failed.zip")
	exportsZip = filepath.Join(tempDir, "exports.zip")

	runsimLogFile, err = os.OpenFile(filepath.Join(tempDir, "runsim_log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		log.Fatalf("ERROR: os.OpenFile: %v", err)
	}
	log.SetOutput(io.MultiWriter(os.Stdout, runsimLogFile))

	flag.Parse()
	if flag.NArg() != 3 {
		log.Fatal("ERROR: wrong number of arguments")
	}

	// initialise common test parameters
	blocks = flag.Arg(0)
	period = flag.Arg(1)
	testname = flag.Arg(2)

	if notifyGithub || notifySlack {
		configIntegration()
	}

	seedOverrideList = strings.TrimSpace(seedOverrideList)
	if seedOverrideList != "" {
		seeds, err = buildSeedList(seedOverrideList)
		if err != nil {
			if notifyGithub || notifySlack {
				pushNotification(true, fmt.Sprintf("Host %s: ERROR: buildSeedList: %v", hostId, err))
			}
			log.Fatal(err)
		}
	}

	seedQueue := make(chan Seed, len(seeds))
	for _, seed := range seeds {
		seedQueue <- Seed{
			Num:          seed,
			Stderr:       filepath.Join(tempDir, buildLogFileName(seed)+".stderr"),
			Stdout:       filepath.Join(tempDir, buildLogFileName(seed)+".stdout"),
			ExportParams: filepath.Join(tempDir, fmt.Sprintf("sim_params-%d.json", seed)),
			ExportState:  filepath.Join(tempDir, fmt.Sprintf("sim_state-%d.json", seed)),
		}
	}
	close(seedQueue)

	// jobs cannot be > len(seeds)
	if jobs > len(seeds) {
		jobs = len(seeds)
	}

	// setup signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	results := make(chan Seed, len(seeds))
	go func() {
		<-sigs
		fmt.Println()

		// drain the queue
		log.Printf("Draining seeds queue...")
		for seed := range seedQueue {
			log.Printf("%d", seed.Num)
		}
		log.Printf("Kill all remaining processes...")
		killAllProcs()
		if notifyGithub || notifySlack {
			uploadLogAndExit()
		}
		os.Exit(1)
	}()

	// set up worker pool
	log.Printf("Allocating %d workers...", jobs)
	wg := sync.WaitGroup{}
	for workerID := 0; workerID < jobs; workerID++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()
			worker(workerID, seedQueue, results)
		}(workerID)
	}

	// idiomatic hack required to use wg.Wait() with select
	waitCh := make(chan struct{})
	go func() {
		defer close(waitCh)
		wg.Wait()
	}()

wait:
	for {
		select {
		case <-waitCh:
			break wait
		case <-time.After(1 * time.Minute):
			fmt.Println(".")
		}
	}

	// analyze results and collect the log file handles
	close(results)
	var okSeeds, failedSeeds, exports []string
	for seed := range results {
		if seed.Failed {
			failedSeeds = append(failedSeeds, seed.Stderr, seed.Stdout)
		} else {
			okSeeds = append(okSeeds, seed.Stderr, seed.Stdout)
		}
		exports = append(exports, seed.ExportParams, seed.ExportState)
	}

	if notifyGithub || notifySlack {
		publishResults(okSeeds, failedSeeds, exports)
	}

	if len(failedSeeds) > 0 {
		os.Exit(1)
	}

	os.Exit(0)
}

func worker(id int, seeds <-chan Seed, results chan Seed) {
	log.Printf("[W%d] Worker is up and running", id)
	for seed := range seeds {
		failed := false
		if err := spawnProcess(id, seed); err != nil {
			failed = true
			log.Printf("[W%d] Seed %d: FAILED", id, seed.Num)
			log.Printf("To reproduce run: %s",
				buildCmdString(testname, blocks, period, genesis, seed.ExportState, seed.ExportParams, seed.Num))

			if exitOnFail {
				log.Printf("\bERROR OUTPUT \n\n%s", err)
				panic("halting simulations")
			}
		}
		results <- Seed{
			Num:          seed.Num,
			Stdout:       seed.Stdout,
			Stderr:       seed.Stderr,
			ExportParams: seed.ExportParams,
			ExportState:  seed.ExportState,
			Failed:       failed,
		}
	}
	log.Printf("[W%d] no seeds left, shutting down", id)
}

func spawnProcess(workerID int, seed Seed) (err error) {
	stderrFile, err := os.Create(seed.Stderr)
	if err != nil {
		if notifyGithub || notifySlack {
			pushNotification(true, fmt.Sprintf("Host %s: ERROR: os.Create: %v", hostId, err))
		}
		log.Fatal(err)
	}

	stdoutFile, err := os.Create(seed.Stdout)
	if err != nil {
		if notifyGithub || notifySlack {
			pushNotification(true, fmt.Sprintf("Host %s: ERROR: os.Create: %v", hostId, err))
		}
		log.Fatal(err)
	}

	s := buildCmdString(testname, blocks, period, genesis, seed.ExportState, seed.ExportParams, seed.Num)
	cmd := execCmd(s)
	cmd.Stdout = stdoutFile

	var stderr io.ReadCloser
	if !exitOnFail {
		cmd.Stderr = stderrFile
	} else {
		if stderr, err = cmd.StderrPipe(); err != nil {
			return err
		}
	}
	sc := bufio.NewScanner(stderr)

	if err = cmd.Start(); err != nil {
		log.Printf("couldn't start %q", s)
		return err
	}
	log.Printf("[W%d] Spawned simulation with pid %d [seed=%d stdout=%s stderr=%s]",
		workerID, cmd.Process.Pid, seed.Num, seed.Stdout, seed.Stderr)
	pushProcess(cmd.Process)
	defer popProcess(cmd.Process)

	if err = cmd.Wait(); err != nil {
		fmt.Printf("%s\n", err)
	}

	if exitOnFail {
		for sc.Scan() {
			fmt.Printf("stderr: %s\n", sc.Text())
		}
	}
	return err
}

func pushProcess(proc *os.Process) {
	mutex.Lock()
	defer mutex.Unlock()
	procs[proc.Pid] = proc
}

func popProcess(proc *os.Process) {
	mutex.Lock()
	defer mutex.Unlock()
	if _, ok := procs[proc.Pid]; ok {
		delete(procs, proc.Pid)
	}
}

func killAllProcs() {
	mutex.Lock()
	defer mutex.Unlock()
	for _, proc := range procs {
		checkSignal(proc, syscall.SIGTERM)
		checkSignal(proc, syscall.SIGKILL)
	}
}

func checkSignal(proc *os.Process, signal syscall.Signal) {
	if err := proc.Signal(signal); err != nil {
		log.Printf("Failed to send %s to PID %d", signal, proc.Pid)
	}
}

func buildCmdString(testName, blocks, period, genesis, exportStatePath, exportParamsPath string, seed int) string {
	return fmt.Sprintf("go test %s -run %s -Enabled=true -NumBlocks=%s -Genesis=%s -Verbose=true "+
		"-Commit=true -Seed=%d -Period=%s -ExportParamsPath %s -ExportStatePath %s -v -timeout %s",
		pkgName, testName, blocks, genesis, seed, period, exportParamsPath, exportStatePath, timeout)
}

func execCmd(cmdStr string) *exec.Cmd {
	cmdSlice := strings.Split(cmdStr, " ")
	return exec.Command(cmdSlice[0], cmdSlice[1:]...)
}

func buildSeedList(seeds string) ([]int, error) {
	strSeedsLst := strings.Split(seeds, ",")
	if len(strSeedsLst) == 0 {
		return nil, fmt.Errorf("seeds was empty")
	}
	intSeeds := make([]int, len(strSeedsLst))
	for i, seedStr := range strSeedsLst {
		intSeed, err := strconv.Atoi(strings.TrimSpace(seedStr))
		if err != nil {
			return nil, fmt.Errorf("cannot convert seed to integer: %v", err)
		}
		intSeeds[i] = intSeed
	}
	return intSeeds, nil
}

func buildLogFileName(seed int) string {
	return fmt.Sprintf("app-simulation-seed-%d-date-%s", seed, time.Now().Format("01-02-2006_150405"))
}
