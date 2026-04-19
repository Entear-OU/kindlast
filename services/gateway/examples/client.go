package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

const baseURL = "http://localhost:8080"

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

type AuthResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	User         interface{} `json:"user"`
}

type QueryRequest struct {
	Query   string                 `json:"query"`
	Options map[string]interface{} `json:"options,omitempty"`
}

func main() {
	// Example 1: Register a new user
	fmt.Println("=== Registering new user ===")
	authResp, err := register("demo@example.com", "password123", "Demo User")
	if err != nil {
		log.Printf("Registration failed: %v", err)
	} else {
		fmt.Printf("Registered successfully!\n")
		fmt.Printf("Access Token: %s...\n", authResp.AccessToken[:50])
	}

	// Example 2: Login
	fmt.Println("\n=== Logging in ===")
	authResp, err = login("demo@example.com", "password123")
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}
	fmt.Printf("Logged in successfully!\n")
	fmt.Printf("Access Token: %s...\n", authResp.AccessToken[:50])

	accessToken := authResp.AccessToken

	// Example 3: Get current user
	fmt.Println("\n=== Getting current user ===")
	user, err := getCurrentUser(accessToken)
	if err != nil {
		log.Printf("Get user failed: %v", err)
	} else {
		fmt.Printf("Current user: %+v\n", user)
	}

	// Example 4: Get plan details
	fmt.Println("\n=== Getting plan details ===")
	plan, err := getPlanDetails(accessToken)
	if err != nil {
		log.Printf("Get plan failed: %v", err)
	} else {
		fmt.Printf("Plan details: %+v\n", plan)
	}

	// Example 5: Send a query
	fmt.Println("\n=== Sending query to RAG service ===")
	err = sendQuery(accessToken, "What are the GDPR requirements?")
	if err != nil {
		log.Printf("Query failed: %v", err)
	}

	// Example 6: Health check
	fmt.Println("\n=== Checking service health ===")
	health, err := healthCheck()
	if err != nil {
		log.Printf("Health check failed: %v", err)
	} else {
		fmt.Printf("Health: %+v\n", health)
	}
}

func register(email, password, fullName string) (*AuthResponse, error) {
	reqBody := RegisterRequest{
		Email:    email,
		Password: password,
		FullName: fullName,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, err
	}

	return &authResp, nil
}

func login(email, password string) (*AuthResponse, error) {
	reqBody := map[string]string{
		"email":    email,
		"password": password,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, err
	}

	return &authResp, nil
}

func getCurrentUser(accessToken string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return user, nil
}

func getPlanDetails(accessToken string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/users/me/plan", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var plan map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		return nil, err
	}

	return plan, nil
}

func sendQuery(accessToken, query string) error {
	reqBody := QueryRequest{
		Query: query,
		Options: map[string]interface{}{
			"max_results": 5,
		},
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/query", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Print rate limit headers
	fmt.Printf("Rate Limit: %s\n", resp.Header.Get("X-RateLimit-Limit"))
	fmt.Printf("Rate Remaining: %s\n", resp.Header.Get("X-RateLimit-Remaining"))
	fmt.Printf("Usage Limit: %s\n", resp.Header.Get("X-Usage-Limit"))
	fmt.Printf("Usage Remaining: %s\n", resp.Header.Get("X-Usage-Remaining"))

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	fmt.Println("Query sent successfully!")
	return nil
}

func healthCheck() (map[string]interface{}, error) {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, err
	}

	return health, nil
}
