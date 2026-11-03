package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"brevity/internal/handlers"
	"github.com/pashagolub/pgxmock/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestRegisterHandler_Success(t *testing.T) {
	// 1. Create a mock database connection
	mockDB, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer mockDB.Close()

	// 2. Expect the INSERT query and mock successful execution
	mockDB.ExpectExec("INSERT INTO users").
		WithArgs("test@example.com", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// 3. Instantiate handler with mock DB
	handler := &handlers.AuthHandler{DB: mockDB}

	// 4. Construct HTTP request body
	payload := map[string]string{
		"email":    "test@example.com",
		"password": "securepassword123",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	// 5. Invoke handler
	handler.Register(rec, req)

	// 6. Assertions
	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201 Created; got %d", rec.Code)
	}

	if err := mockDB.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled db expectations: %v", err)
	}
}

func TestLoginHandler_Success(t *testing.T) {
	// 1. Create a mock database connection
	mockDB, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer mockDB.Close()

	// 2. Hash a dummy password for comparison
	rawPassword := "securepassword123"
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(rawPassword), bcrypt.DefaultCost)

	// 3. Expect the SELECT query and return mocked user row
	rows := pgxmock.NewRows([]string{"id", "password_hash"}).
		AddRow(1, string(hashedPassword))

	mockDB.ExpectQuery("SELECT id, password_hash FROM users").
		WithArgs("test@example.com").
		WillReturnRows(rows)

	// 4. Instantiate handler with mock DB
	handler := &handlers.AuthHandler{DB: mockDB}

	// 5. Construct HTTP request body
	payload := map[string]string{
		"email":    "test@example.com",
		"password": rawPassword,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	// 6. Invoke handler
	handler.Login(rec, req)

	// 7. Assertions
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 OK; got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if token, exists := resp["token"]; !exists || token == "" {
		t.Errorf("expected non-empty JWT token in response")
	}

	if err := mockDB.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled db expectations: %v", err)
	}
}
