package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/cors"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/stdlib"
	"github.com/ulule/limiter/v3/drivers/store/memory"

	"napi/components"
)

var (
	store      *sessions.CookieStore
	VERSION    string
	USERNAME   string
	PASSWORD   string
	VERBOSE_LOG bool
)

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Error loading .env file: %v", err)
	}

	// Initialize session store with key from environment variable
	key := []byte(os.Getenv("SESSION_KEY"))
	store = sessions.NewCookieStore(key)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   0,        // Session cookie expires when the browser closes
		HttpOnly: true,     // Prevent JavaScript access to the cookie
		Secure:   true,     // Ensure the cookie is only sent over HTTPS
	}

	// Load credentials and version from environment variables
	VERSION = os.Getenv("VERSION")
	USERNAME = os.Getenv("USERNAME")
	PASSWORD = os.Getenv("PASSWORD")

	// Load verbose logging flag
	VERBOSE_LOG = true

	// Ensure required environment variables are set
	if USERNAME == "" || PASSWORD == "" || VERSION == "" {
		log.Fatal("Missing required environment variables: USERNAME, PASSWORD, VERSION")
	}

	// Set up logging to file
	setupLogging()
}

// setupLogging initializes logging to a file, appending to it if it exists
func setupLogging() {
	logFile, err := os.OpenFile("serve.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)
	log.Printf("Server started at: %s", time.Now().Format(time.RFC3339))
}

func main() {
	serverUsername := os.Getenv("USER")
	corsOptions := cors.Options{
		AllowedOrigins:   []string{"https://ncwi." + serverUsername + ".hackclub.app", "http://localhost"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}

	r := mux.NewRouter()

	// Apply CORS middleware
	r.Use(cors.Handler(corsOptions))

	// Apply security headers middleware
	r.Use(securityHeadersMiddleware)

	// General rate limiter configuration for all routes except login
	generalRate := limiter.Rate{
		Period: 1 * time.Minute,
		Limit:  10,
	}
	generalLimiterStore := memory.NewStore()
	generalLimiter := limiter.New(generalLimiterStore, generalRate)
	generalLimiterMiddleware := stdlib.NewMiddleware(generalLimiter)

	// Rate limiter configuration for login route
	loginRate := limiter.Rate{
		Period: 10 * time.Minute,
		Limit:  25,
	}
	loginLimiterStore := memory.NewStore()
	loginLimiter := limiter.New(loginLimiterStore, loginRate)
	loginLimiterMiddleware := stdlib.NewMiddleware(loginLimiter)

	// Login endpoint with specific rate limiter
	r.Handle("/login", loginLimiterMiddleware.Handler(http.HandlerFunc(loginHandler))).Methods("POST", "OPTIONS")

	// Protected routes
	r.Handle("/version", isAuthenticated(http.HandlerFunc(versionHandler))).Methods("GET", "OPTIONS")

	// Handle preflight requests
	r.HandleFunc("/login", optionsHandler).Methods("OPTIONS")
	r.HandleFunc("/version", optionsHandler).Methods("OPTIONS")

	// Apply general rate limiting to all routes except login
	r.Use(generalLimiterMiddleware.Handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "5499"
	}

	websocketPort := os.Getenv("WEBSOCKET_PORT")
	if websocketPort == "" {
		websocketPort = "5498"
	}

	// Start HTTP API server
	go func() {
		log.Printf("API server is running on port %s", port)
		log.Fatal(http.ListenAndServe(":"+port, r))
	}()

	// Start WebSocket server
	go func() {
		go components.StartWebSocketServer()
		// websocketRouter := mux.NewRouter()
		// Add WebSocket handler here
		// log.Printf("WebSocket server is running on port %s", websocketPort)
		// log.Fatal(http.ListenAndServe(":"+websocketPort, websocketRouter))
	}()

	// Block the main goroutine
	select {}
}

// Handles the login requests and validates the user credentials
func loginHandler(w http.ResponseWriter, r *http.Request) {
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	// Decode the JSON request payload
	err := json.NewDecoder(r.Body).Decode(&creds)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Validate the provided credentials
	if creds.Username == USERNAME && creds.Password == PASSWORD {
		// Invalidate previous session if exists
		session, _ := store.Get(r, "session")
		session.Values = make(map[interface{}]interface{})
		session.Save(r, w)

		// Create a new session
		session, _ = store.Get(r, "session")
		session.Values["user"] = USERNAME
		session.Save(r, w)

		if VERBOSE_LOG {
			log.Printf("User %s logged in at %s", USERNAME, time.Now().Format(time.RFC3339))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message":   "Login successful",
			"sessionId": session.ID,
		})
	} else {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
	}
}

// Middleware to check if the user is authenticated
func isAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if session.Values["user"] != nil {
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	})
}

// Handles requests to retrieve the version
func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version": VERSION,
	})
}

// Handles OPTIONS requests for CORS preflight
func optionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusNoContent)
}

// Middleware to set security headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}
