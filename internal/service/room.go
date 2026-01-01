package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	"github.com/yourusername/chat-app/internal/biz"
	"github.com/yourusername/chat-app/internal/middleware"
)

// RoomService implements the room service
type RoomService struct {
	chatV1.UnimplementedRoomServiceServer

	uc  *biz.RoomUseCase
	log *log.Helper
}

// NewRoomService creates a new room service
func NewRoomService(uc *biz.RoomUseCase, logger log.Logger) *RoomService {
	return &RoomService{
		uc:  uc,
		log: log.NewHelper(log.With(logger, "module", "service/room")),
	}
}

// CreateRoom creates a new chat room
func (s *RoomService) CreateRoom(ctx context.Context, req *chatV1.CreateRoomRequest) (*chatV1.Room, error) {
	// Get user ID from context (set by authentication middleware)
	userID, err := s.getUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	room, err := s.uc.CreateRoom(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	return &chatV1.Room{
		Id:          room.ID,
		Name:        room.Name,
		Description: room.Description,
		Type:        room.Type,
		CreatedBy:   room.CreatedBy,
		CreatedAt:   room.CreatedAt.Unix(),
	}, nil
}

// GetRoom retrieves room information by ID
func (s *RoomService) GetRoom(ctx context.Context, req *chatV1.GetRoomRequest) (*chatV1.Room, error) {
	// Get user ID from context
	userID, err := s.getUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	room, err := s.uc.GetRoom(ctx, userID, req.Id)
	if err != nil {
		return nil, err
	}

	return &chatV1.Room{
		Id:          room.ID,
		Name:        room.Name,
		Description: room.Description,
		Type:        room.Type,
		CreatedBy:   room.CreatedBy,
		CreatedAt:   room.CreatedAt.Unix(),
	}, nil
}

// ListRooms lists rooms for a user
func (s *RoomService) ListRooms(ctx context.Context, req *chatV1.ListRoomsRequest) (*chatV1.ListRoomsResponse, error) {
	// Get user ID from context or use provided user_id (if authorized)
	userID := req.UserId
	if userID == 0 {
		var err error
		userID, err = s.getUserIDFromContext(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		// If requesting another user's rooms, check authorization
		contextUserID, _ := s.getUserIDFromContext(ctx)
		if contextUserID != userID {
			// In a real app, check if user has permission to view other user's rooms
			return nil, biz.ErrRoomAccessDenied
		}
	}

	// Set defaults for pagination
	limit := req.Limit
	if limit == 0 || limit > 100 {
		limit = 20 // Default limit
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	rooms, total, err := s.uc.ListUserRooms(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}

	var responseRooms []*chatV1.Room
	for _, room := range rooms {
		responseRooms = append(responseRooms, &chatV1.Room{
			Id:          room.ID,
			Name:        room.Name,
			Description: room.Description,
			Type:        room.Type,
			CreatedBy:   room.CreatedBy,
			CreatedAt:   room.CreatedAt.Unix(),
		})
	}

	return &chatV1.ListRoomsResponse{
		Rooms: responseRooms,
		Total: total,
	}, nil
}

// JoinRoom allows a user to join a room
func (s *RoomService) JoinRoom(ctx context.Context, req *chatV1.JoinRoomRequest) (*chatV1.JoinRoomResponse, error) {
	// Get user ID from context or use provided user_id
	userID := req.UserId
	if userID == 0 {
		var err error
		userID, err = s.getUserIDFromContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	room, err := s.uc.JoinRoom(ctx, userID, req.RoomId)
	if err != nil {
		return nil, err
	}

	return &chatV1.JoinRoomResponse{
		Success: true,
		Room: &chatV1.Room{
			Id:          room.ID,
			Name:        room.Name,
			Description: room.Description,
			Type:        room.Type,
			CreatedBy:   room.CreatedBy,
			CreatedAt:   room.CreatedAt.Unix(),
		},
	}, nil
}

// LeaveRoom allows a user to leave a room
func (s *RoomService) LeaveRoom(ctx context.Context, req *chatV1.LeaveRoomRequest) (*chatV1.LeaveRoomResponse, error) {
	// Get user ID from context or use provided user_id
	userID := req.UserId
	if userID == 0 {
		var err error
		userID, err = s.getUserIDFromContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	err := s.uc.LeaveRoom(ctx, userID, req.RoomId)
	if err != nil {
		return nil, err
	}

	return &chatV1.LeaveRoomResponse{
		Success: true,
	}, nil
}

// getUserIDFromContext extracts user ID from request context
// This would be set by an authentication middleware
func (s *RoomService) getUserIDFromContext(ctx context.Context) (int64, error) {
	userID, ok := ctx.Value(middleware.UserIDKey).(int64)
	if !ok {
		return 0, biz.ErrUserNotFound
	}

	if userID <= 0 {
		return 0, biz.ErrUserNotFound
	}

	return userID, nil
}
