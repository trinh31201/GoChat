package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
)

func main() {
	// Connect to gRPC server
	conn, err := grpc.NewClient("localhost:9000", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close connection: %v", err)
		}
	}()

	// Create service clients
	userClient := userV1.NewUserServiceClient(conn)
	roomClient := chatV1.NewRoomServiceClient(conn)
	_ = chatV1.NewChatServiceClient(conn) // For future use

	ctx := context.Background()

	// Test user registration
	fmt.Println("=== Testing User Registration ===")
	timestamp := time.Now().Unix()
	registerResp, err := userClient.Register(ctx, &userV1.RegisterRequest{
		Username: fmt.Sprintf("testuser_%d", timestamp),
		Email:    fmt.Sprintf("test_%d@example.com", timestamp),
		Password: "password123",
	})
	if err != nil {
		log.Printf("Registration failed: %v", err)
	} else {
		fmt.Printf("✓ User registered: ID=%d, Username=%s\n", registerResp.User.Id, registerResp.User.Username)
		fmt.Printf("✓ Token: %s...\n\n", registerResp.Token[:20])
	}

	// Test user login
	fmt.Println("=== Testing User Login ===")
	loginResp, err := userClient.Login(ctx, &userV1.LoginRequest{
		Email:    fmt.Sprintf("test_%d@example.com", timestamp),
		Password: "password123",
	})
	if err != nil {
		log.Printf("Login failed: %v", err)
	} else {
		fmt.Printf("✓ Login successful: User ID=%d\n", loginResp.User.Id)
		fmt.Printf("✓ Token: %s...\n\n", loginResp.Token[:20])
	}

	// Get user profile
	fmt.Println("=== Testing Get User Profile ===")
	if registerResp != nil {
		user, err := userClient.GetUser(ctx, &userV1.GetUserRequest{
			Id: registerResp.User.Id,
		})
		if err != nil {
			log.Printf("Get user failed: %v", err)
		} else {
			fmt.Printf("✓ Got user: Username=%s, Email=%s, Status=%s\n\n",
				user.Username, user.Email, user.Status)
		}
	}

	// Update user status
	fmt.Println("=== Testing Update Status ===")
	if registerResp != nil {
		statusResp, err := userClient.UpdateStatus(ctx, &userV1.UpdateStatusRequest{
			UserId: registerResp.User.Id,
			Status: "online",
		})
		if err != nil {
			log.Printf("Update status failed: %v", err)
		} else {
			fmt.Printf("✓ Status updated: %s\n\n", statusResp.Status)
		}
	}

	// Note: Room and Chat operations require authentication context
	// In a real application, you would add the JWT token to the context
	fmt.Println("=== Room and Chat Operations ===")
	fmt.Println("Note: These operations require authentication middleware")
	fmt.Println("In production, you would pass the JWT token in the request context")

	// Example of how to create a room (will fail without auth)
	roomResp, err := roomClient.CreateRoom(ctx, &chatV1.CreateRoomRequest{
		Name:        fmt.Sprintf("Test Room %d", timestamp),
		Description: "This is a test room",
		Type:        "public",
	})
	if err != nil {
		fmt.Printf("✗ Create room failed (expected without auth): %v\n", err)
	} else {
		fmt.Printf("✓ Room created: ID=%d, Name=%s\n", roomResp.Id, roomResp.Name)
	}

	fmt.Println("\n=== Test Complete ===")
	fmt.Println("To test authenticated endpoints, you need to:")
	fmt.Println("1. Add authentication middleware to the server")
	fmt.Println("2. Pass JWT token in gRPC metadata")
	fmt.Println("3. Or use the HTTP/REST API with Authorization header")
}
