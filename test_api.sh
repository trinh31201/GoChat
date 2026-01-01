
#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Base URLs
HTTP_URL="http://localhost:8001"
GRPC_URL="localhost:9001"

# Test data
TEST_USER="testuser_$(date +%s)"
TEST_EMAIL="test_$(date +%s)@example.com"
TEST_PASSWORD="password123"
TOKEN=""
USER_ID=""

echo -e "${YELLOW}=== Chat App API Testing Script ===${NC}\n"

# Function to test endpoint
test_endpoint() {
    local method=$1
    local endpoint=$2
    local data=$3
    local description=$4
    
    echo -e "${YELLOW}Testing: $description${NC}"
    echo "Endpoint: $method $endpoint"
    if [ ! -z "$data" ]; then
        echo "Data: $data"
    fi
    
    if [ -z "$data" ]; then
        response=$(curl -s -X $method "$HTTP_URL$endpoint" -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN")
    else
        response=$(curl -s -X $method "$HTTP_URL$endpoint" -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" -d "$data")
    fi
    
    echo "Response: $response"
    
    # Check if response contains error
    if echo "$response" | grep -q "error"; then
        echo -e "${RED}✗ Test might have failed${NC}\n"
    else
        echo -e "${GREEN}✓ Test passed${NC}\n"
    fi
    
    echo "---"
    
    # Return response for further processing
    echo "$response"
}

# 1. Test User Registration
echo -e "${GREEN}1. USER REGISTRATION${NC}"
REGISTER_DATA=$(cat <<EOF
{
    "username": "$TEST_USER",
    "email": "$TEST_EMAIL",
    "password": "$TEST_PASSWORD"
}
EOF
)

response=$(test_endpoint "POST" "/api/v1/users/register" "$REGISTER_DATA" "Register new user")

# Extract token from response (basic extraction, might need adjustment)
TOKEN=$(echo $response | grep -o '"token":"[^"]*' | grep -o '[^"]*$' | tail -1)
USER_ID=$(echo $response | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | tail -1)

if [ ! -z "$TOKEN" ]; then
    echo -e "${GREEN}✓ Got token: ${TOKEN:0:20}...${NC}"
    echo -e "${GREEN}✓ Got user ID: $USER_ID${NC}\n"
else
    echo -e "${RED}✗ Failed to get token${NC}\n"
fi

# 2. Test User Login
echo -e "${GREEN}2. USER LOGIN${NC}"
LOGIN_DATA=$(cat <<EOF
{
    "email": "$TEST_EMAIL",
    "password": "$TEST_PASSWORD"
}
EOF
)

response=$(test_endpoint "POST" "/api/v1/users/login" "$LOGIN_DATA" "Login with credentials")

# 3. Get User Profile
echo -e "${GREEN}3. GET USER PROFILE${NC}"
test_endpoint "GET" "/api/v1/users/$USER_ID" "" "Get user profile"

# 4. Update User Status
echo -e "${GREEN}4. UPDATE USER STATUS${NC}"
STATUS_DATA=$(cat <<EOF
{
    "user_id": $USER_ID,
    "status": "online"
}
EOF
)

test_endpoint "PUT" "/api/v1/users/$USER_ID/status" "$STATUS_DATA" "Update user status to online"

# 5. Create a Room
echo -e "${GREEN}5. CREATE ROOM${NC}"
ROOM_DATA=$(cat <<EOF
{
    "name": "Test Room $(date +%s)",
    "description": "This is a test room",
    "type": "public"
}
EOF
)

response=$(test_endpoint "POST" "/api/v1/rooms" "$ROOM_DATA" "Create a new room")
ROOM_ID=$(echo $response | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | tail -1)

if [ ! -z "$ROOM_ID" ]; then
    echo -e "${GREEN}✓ Created room ID: $ROOM_ID${NC}\n"
fi

# 6. Get Room Details
echo -e "${GREEN}6. GET ROOM DETAILS${NC}"
test_endpoint "GET" "/api/v1/rooms/$ROOM_ID" "" "Get room details"

# 7. Send a Message
echo -e "${GREEN}7. SEND MESSAGE${NC}"
MESSAGE_DATA=$(cat <<EOF
{
    "room_id": $ROOM_ID,
    "content": "Hello, this is a test message!",
    "type": "text"
}
EOF
)

response=$(test_endpoint "POST" "/api/v1/messages" "$MESSAGE_DATA" "Send message to room")
MESSAGE_ID=$(echo $response | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | tail -1)

# 8. Get Messages
echo -e "${GREEN}8. GET MESSAGES${NC}"
test_endpoint "GET" "/api/v1/rooms/$ROOM_ID/messages?limit=10" "" "Get messages from room"

# 9. Mark Message as Read
echo -e "${GREEN}9. MARK MESSAGE AS READ${NC}"
READ_DATA=$(cat <<EOF
{
    "message_id": $MESSAGE_ID,
    "user_id": $USER_ID
}
EOF
)

test_endpoint "POST" "/api/v1/messages/$MESSAGE_ID/read" "$READ_DATA" "Mark message as read"

# 10. List User's Rooms
echo -e "${GREEN}10. LIST USER ROOMS${NC}"
test_endpoint "GET" "/api/v1/users/$USER_ID/rooms" "" "List rooms for user"

echo -e "\n${GREEN}=== Testing Complete ===${NC}"
echo -e "User: $TEST_EMAIL"
echo -e "Token: ${TOKEN:0:30}..."
echo -e "Room ID: $ROOM_ID"