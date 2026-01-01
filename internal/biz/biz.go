package biz

import (
	"github.com/google/wire"
)

// ProviderSet is biz providers.
var ProviderSet = wire.NewSet(
	// Auth components
	NewJWTTokenManagerFromConfig,
	NewBcryptPasswordHasher,

	// Wire interfaces
	wire.Bind(new(TokenManager), new(*JWTTokenManager)),
	wire.Bind(new(PasswordHasher), new(*BcryptPasswordHasher)),

	// Use cases
	NewUserUseCase,
	NewRoomUseCase,
	NewChatUseCase,
)
