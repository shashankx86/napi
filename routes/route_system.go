// routes/routes_system.go

package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"time"
	// "regexp"
	"fmt"
	"strings"

	"github.com/gorilla/mux"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/stdlib"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

var (
	systemLimiterStore = memory.NewStore()
	systemRate         = limiter.Rate{
		Period: 1 * time.Minute,
		Limit:  70,
	}
	systemLimiter       = limiter.New(systemLimiterStore, systemRate)
	systemLimiterMiddleware = stdlib.NewMiddleware(systemLimiter)
)

type Unit struct {
	UNIT        string `json:"UNIT"`
	LOAD        string `json:"LOAD"`
	ACTIVE      string `json:"ACTIVE"`
	SUB         string `json:"SUB"`
	DESCRIPTION string `json:"DESCRIPTION"`
}

func executeCommand(command string) (string, error) {
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}


func parseUnits(data, unitType string) ([]Unit, error) {
	lines := strings.Split(data, "\n")
	units := []Unit{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.Contains(fields[0], unitType) {
			continue
		}
		unit := Unit{
			UNIT:        fields[0],
			LOAD:        fields[1],
			ACTIVE:      fields[2],
			SUB:         fields[3],
			DESCRIPTION: strings.Join(fields[4:], " "),
		}
		units = append(units, unit)
	}
	return units, nil
}

func ListServices(w http.ResponseWriter, r *http.Request) {
	serviceStdout, err := executeCommand("systemctl --user list-units --type=service --all")
	if err != nil {
		http.Error(w, "Error fetching services", http.StatusInternalServerError)
		return
	}
	services, err := parseUnits(serviceStdout, ".service")
	if err != nil {
		http.Error(w, "Error parsing services output", http.StatusInternalServerError)
		return
	}

	socketStdout, err := executeCommand("systemctl --user list-units --type=socket --all")
	if err != nil {
		http.Error(w, "Error fetching sockets", http.StatusInternalServerError)
		return
	}
	sockets, err := parseUnits(socketStdout, ".socket")
	if err != nil {
		http.Error(w, "Error parsing sockets output", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"services": services,
		"sockets":  sockets,
	})
}

func StartService(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("target")
	if service == "" {
		http.Error(w, "Service name is required", http.StatusBadRequest)
		return
	}

	err := exec.Command("systemctl", "--user", "start", service).Run()
	if err != nil {
		http.Error(w, "Error starting service "+service, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Service " + service + " started successfully",
	})
}

func StopService(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("target")
	if service == "" {
		http.Error(w, "Service name is required", http.StatusBadRequest)
		return
	}

	err := exec.Command("systemctl", "--user", "stop", service).Run()
	if err != nil {
		http.Error(w, "Error stopping service "+service, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Service " + service + " stopped successfully",
	})
}

func RestartService(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("target")
	if service == "" {
		http.Error(w, "Service name is required", http.StatusBadRequest)
		return
	}

	err := exec.Command("systemctl", "--user", "restart", service).Run()
	if err != nil {
		http.Error(w, "Error restarting service "+service, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Service " + service + " restarted successfully",
	})
}

func WriteFile(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	filepath := r.URL.Query().Get("filepath")
	filecontent := r.URL.Query().Get("filecontent")
	if filename == "" || filepath == "" || filecontent == "" {
		http.Error(w, "Filename, filepath, and filecontent are required", http.StatusBadRequest)
		return
	}

	fullPath := filepath + "/" + filename
	err := os.WriteFile(fullPath, []byte(filecontent), 0644)
	if err != nil {
		http.Error(w, "Error saving file "+filename+" at "+filepath, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "File " + filename + " saved successfully at " + filepath,
	})
}

func ReadFile(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	filepath := r.URL.Query().Get("filepath")
	if filename == "" || filepath == "" {
		http.Error(w, "Filename and filepath are required", http.StatusBadRequest)
		return
	}

	fullPath := filepath + "/" + filename
	fileContent, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "Error reading file "+filename+" at "+filepath, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"content": string(fileContent),
	})
}

func ScheduleTask(w http.ResponseWriter, r *http.Request) {
	time := r.URL.Query().Get("time")
	command := r.URL.Query().Get("command")
	if time == "" || command == "" {
		http.Error(w, "Both time and command are required", http.StatusBadRequest)
		return
	}

	atCommand := fmt.Sprintf(`echo "%s" | at %s`, command, time)
	_, err := executeCommand(atCommand)
	if err != nil {
		http.Error(w, "Error scheduling task at "+time, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Task scheduled at " + time,
	})
}

func RegisterSystemRoutes(r *mux.Router) {
	systemRouter := r.PathPrefix("/system").Subrouter()

	systemRouter.Use(systemLimiterMiddleware.Handler)

	systemRouter.HandleFunc("/services", ListServices).Methods("GET")
	systemRouter.HandleFunc("/services/start", StartService).Methods("POST")
	systemRouter.HandleFunc("/services/stop", StopService).Methods("POST")
	systemRouter.HandleFunc("/services/restart", RestartService).Methods("POST")
	systemRouter.HandleFunc("/write", WriteFile).Methods("POST")
	systemRouter.HandleFunc("/read", ReadFile).Methods("GET")
	systemRouter.HandleFunc("/at", ScheduleTask).Methods("POST")
}
