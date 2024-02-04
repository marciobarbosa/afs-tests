package main

import (
    "fmt"
    "os"
    "time"
    "strings"
    "strconv"
    "os/exec"
    "math/rand"
)

type Cell struct {
    Name string
    Primary bool
    RO bool
    RW bool
}

const (
    SETCELL = iota
    SYSCTL
)

func CreateCSDB(cellnames []string, ncells int) {
    csdb, err := os.Create("/tmp/CellServDB")
    if err != nil {
	fmt.Println("Error creating CellServDB:", err)
	os.Exit(1)
    }

    // primary cell
    _, err = fmt.Fprintf(csdb, ">robotest\n192.168.2.129\t\t#vagrant\n")
    if err != nil {
	fmt.Println("Error adding cells to CellServDB:", err)
	os.Exit(1)
    }

    // remote cells
    for _, cellname := range cellnames {
	_, err := fmt.Fprintf(csdb, ">%s\n192.168.2.138\t\t#server\n", cellname)
	if err != nil {
	    fmt.Println("Error adding cells to CellServDB:", err)
	    os.Exit(1)
	}
    }
    csdb.Close()
}

func SetupEnv(cellnames []string, ncells int) {
    // Start the client
    cmd := exec.Command("afsrobot", "setup")
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error starting the client:", err)
	os.Exit(1)
    }

    // create csdb file from those names
    CreateCSDB(cellnames, ncells)

    // copy csdb to the proper directory
    cmd = exec.Command("sudo", "cp", "/tmp/CellServDB", "/usr/vice/etc/CellServDB.local")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error copying CellServDB:", err)
	os.Exit(1)
    }

    // restart the client so the new csdb can be loaded
    cmd = exec.Command("sudo", "/etc/init.d/openafs-client", "restart")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Could not restart the client:", err)
	os.Exit(1)
    }

    // get admin tokens
    cmd = exec.Command("afsrobot", "login")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Could not get tokens:", err)
	os.Exit(1)
    }
}

func SetCell(cell Cell, rw int, ro int) {
    fs := "/usr/afs/bin/fs"
    rw_opt := "off"
    ro_opt := "off"

    if rw == 1 {
	rw_opt = "on"
    }
    if ro == 1 {
	ro_opt = "on"
    }

    cmd := exec.Command("sudo", fs, "setcell", cell.Name, "-hardmount-rw", rw_opt, "-hardmount-ro", ro_opt)
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error running setcell:", err)
	os.Exit(1)
    }
}

func GetCurrentState() (int, int, int) {
    cmd := exec.Command("sudo", "sysctl", "afs.hm_retry_RW")
    output, err := cmd.Output()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    sysctl_output := string(output)
    index := strings.Index(sysctl_output, "=")
    rw, _ := strconv.Atoi(strings.TrimSpace(sysctl_output[index+2:]))

    cmd = exec.Command("sudo", "sysctl", "afs.hm_retry_RO")
    output, err = cmd.Output()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    sysctl_output = string(output)
    index = strings.Index(sysctl_output, "=")
    ro, _ := strconv.Atoi(strings.TrimSpace(sysctl_output[index+2:]))

    cmd = exec.Command("sudo", "sysctl", "afs.hm_retry_int")
    output, err = cmd.Output()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    sysctl_output = string(output)
    index = strings.Index(sysctl_output, "=")
    interval, _ := strconv.Atoi(strings.TrimSpace(sysctl_output[index+2:]))

    return rw, ro, interval
}

func SysCtl(cell Cell, rw int, ro int) {
    cur_rw, cur_ro, _ := GetCurrentState()

    rw_opt := "afs.hm_retry_RW=0"
    ro_opt := "afs.hm_retry_RO=0"

    if rw == 1 {
	rw_opt = "afs.hm_retry_RW=1"
    }
    if ro == 1 {
	ro_opt = "afs.hm_retry_RO=1"
    }

    cmd := exec.Command("sudo", "sysctl", "-w", rw_opt)
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }
    cmd = exec.Command("sudo", "sysctl", "-w", ro_opt)
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    if cell.Primary {
	return
    }
    // for non-primary cells, set hm_retry_remote_all, evaluate its state, set
    // hm_retry_remote_all back to 0, and restore the previous states so the
    // primary cell isn't affected.
    cmd = exec.Command("sudo", "sysctl", "-w", "afs.hm_retry_remote_all=1")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }
    EvalStates(cell)

    cmd = exec.Command("sudo", "sysctl", "-w", "afs.hm_retry_remote_all=0")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    // restore previous states so primary cell isn't affected
    rw_opt = "afs.hm_retry_RW=0"
    ro_opt = "afs.hm_retry_RO=0"

    if cur_rw == 1 {
	rw_opt = "afs.hm_retry_RW=1"
    }
    if cur_ro == 1 {
	ro_opt = "afs.hm_retry_RO=1"
    }

    cmd = exec.Command("sudo", "sysctl", "-w", rw_opt)
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }

    cmd = exec.Command("sudo", "sysctl", "-w", ro_opt)
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }
}

// Cell states set through sysctl are set lazily. This function forces those
// states to be applied immediately.
//
// Parameters:
//   cell: cell where states should be applied
func EvalStates(cell Cell) {
    fs := "/usr/afs/bin/fs"
    cmd := exec.Command(fs, "getcell", cell.Name)
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error running getcell:", err)
	os.Exit(1)
    }
}

func SetInterval(interval int) {
    int_opt := fmt.Sprintf("afs.hm_retry_int=%d", interval)
    cmd := exec.Command("sudo", "sysctl", "-w", int_opt)
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error running sysctl:", err)
	os.Exit(1)
    }
}

func SetState(method int, cell *Cell, interval *int, rw int, ro int) {
    cell.RW = false
    cell.RO = false

    if *interval == 0 {
	if method == SYSCTL {
	    return
	}
	if rw != 0 || ro != 0 {
	    *interval = 60
	}
    }
    if rw == 1 {
	cell.RW = true
    }
    if ro == 1 {
	cell.RO = true
    }
}

func GetState(cell Cell) Cell {
    fs := "/usr/afs/bin/fs"
    cmd := exec.Command(fs, "getcell", cell.Name)

    output, err := cmd.Output()
    if err != nil {
	fmt.Println("Error running getcell:", err)
	os.Exit(1)
    }
    cellstate := Cell{
	Name:    cell.Name,
	Primary: cell.Primary,
	RO:      false,
	RW:      false,
    }
    states := string(output)
    rw_state := "hard-mount for read-write volumes enabled"
    ro_state := "hard-mount for read-only volumes enabled"

    if strings.Contains(states, rw_state) {
	cellstate.RW = true
    }
    if strings.Contains(states, ro_state) {
	cellstate.RO = true
    }
    return cellstate
}

func RunTests(cells []Cell, ncells int, nruns int) {
    var cellstates []Cell
    var commands []string

    rand.Seed(time.Now().UnixNano())

    for run_i := 0; run_i < nruns ; run_i++ {
	method := rand.Intn(2)
	interval:= rand.Intn(4)

	switch method {
	case SETCELL:
	    cell_i := rand.Intn(ncells)
	    rw := rand.Intn(2)
	    ro := rand.Intn(2)

	    SetInterval(interval)
	    SetCell(cells[cell_i], rw, ro)
	    SetState(SETCELL, &cells[cell_i], &interval, rw, ro)
	    EvalStates(cells[cell_i])
	    if interval == 60 && !cells[cell_i].Primary {
		// hm_retry_* globals have been reset
		primary_i := 0
		cells[primary_i].RW = false
		cells[primary_i].RO = false
	    }

	    cname := cells[cell_i].Name
	    command := fmt.Sprintf("setcell %s rw=%d ro=%d int=%d", cname, rw, ro, interval)
	    commands = append(commands, command)

	case SYSCTL:
	    cell_i := rand.Intn(ncells)
	    rw := rand.Intn(2)
	    ro := rand.Intn(2)

	    SetInterval(interval)
	    SysCtl(cells[cell_i], rw, ro)
	    SetState(SYSCTL, &cells[cell_i], &interval, rw, ro)
	    EvalStates(cells[cell_i])

	    if interval == 0 && cells[cell_i].Primary {
		SysCtl(cells[cell_i], 0, 0)
	    }
	    cname := cells[cell_i].Name
	    command := fmt.Sprintf("sysctl %s rw=%d ro=%d int=%d", cname, rw, ro, interval)
	    commands = append(commands, command)
	}
    }

    failed := false
    SetInterval(1) // so getcell doesn't change the final states
    for _, cell := range cells {
	cellstate := GetState(cell)
	cellstates = append(cellstates, cellstate)
	if cell.RO != cellstate.RO || cell.RW != cellstate.RW {
	    fmt.Printf("Cell name: %s\n", cell.Name)
	    fmt.Printf("Expected RW state: %v, Current RW state: %v\n", cell.RW, cellstate.RW)
	    fmt.Printf("Expected RO state: %v, Current RO state: %v\n", cell.RO, cellstate.RO)
	    fmt.Printf("--------------------------------------------\n\n")
	    failed = true
	}
    }

    if failed {
	fmt.Printf("Commands:\n\n")
	for _, cmd := range commands {
	    fmt.Println(cmd)
	}
	fmt.Printf("\n[FAILED]\n")
	return
    }
    fmt.Printf("[SUCCESS]\n")
}

func NukeEnv() {
    cmd := exec.Command("afsrobot", "teardown")
    err := cmd.Run()
    if err != nil {
	fmt.Println("Error destroying client:", err)
	os.Exit(1)
    }
    cmd = exec.Command("rm", "/tmp/CellServDB")
    err = cmd.Run()
    if err != nil {
	fmt.Println("Error removing CellServDB:", err)
	os.Exit(1)
    }
}

func main() {
    var cells []Cell
    var cellnames []string

    if len(os.Args) < 4 {
	fmt.Println("Usage:", os.Args[0], "-ncells <number_of_cells> -nruns <number_of_runs>")
	return
    }
    if os.Args[1] != "-ncells" {
	fmt.Println("Usage:", os.Args[0], "-ncells <number_of_cells> -nruns <number_of_runs>")
	return
    }
    if os.Args[3] != "-nruns" {
	fmt.Println("Usage:", os.Args[0], "-ncells <number_of_cells> -nruns <number_of_runs>")
	return
    }

    ncells, err := strconv.Atoi(os.Args[2])
    if err != nil {
	fmt.Println("Error parsing -ncells argument:", err)
	return
    }
    nruns, err := strconv.Atoi(os.Args[4])
    if err != nil {
	fmt.Println("Error parsing -nruns argument:", err)
	return
    }

    cell := Cell{
	Name:    "robotest",
	Primary: true,
	RO:      false,
	RW:      false,
    }
    cells = append(cells, cell)

    // generate fake remote cell names
    for i := 1; i <= (ncells-1); i++ {
	cellname := fmt.Sprintf("cellname_%d", i)
	cellnames = append(cellnames, cellname)

	cell = Cell{
	    Name:    cellname,
	    Primary: false,
	    RO:      false,
	    RW:      false,
	}
	cells = append(cells, cell)
    }

    SetupEnv(cellnames, ncells)
    RunTests(cells, ncells, nruns)
    NukeEnv()
}
