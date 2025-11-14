// Configuration
const API_URL = 'https://chat-app-production-d5c0.up.railway.app/api/v1';
const WS_URL = 'wss://chat-app-production-d5c0.up.railway.app/ws';

// Global State
let ws = null;
let currentRoom = null;
let rooms = [];
let token = localStorage.getItem('token');
let userId = localStorage.getItem('userId');
let username = localStorage.getItem('username');

// Check authentication
if (!token || !userId || !username) {
    window.location.href = '/web/login.html';
}

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('currentUsername').textContent = username;
    loadRooms();
    initializeWebSocket();
    setupEventListeners();
});

// ==================== WEBSOCKET ====================
function initializeWebSocket() {
    ws = new WebSocket(WS_URL);

    ws.onopen = () => {
        console.log('WebSocket connected');
        // Authenticate
        ws.send(JSON.stringify({
            type: 'auth',
            token: token
        }));
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            handleWebSocketMessage(data);
        } catch (error) {
            console.error('Failed to parse WebSocket message:', error);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        showAlert('Connection error', 'error');
    };

    ws.onclose = () => {
        console.log('WebSocket disconnected');
        // Reconnect after 3 seconds
        setTimeout(initializeWebSocket, 3000);
    };
}

function handleWebSocketMessage(data) {
    console.log('WebSocket message:', data);

    switch (data.type) {
        case 'success':
        case 'error':
            console.log(data.message);
            break;

        case 'room_joined':
            console.log('Successfully joined room', data.room_id);
            break;

        case 'new_message':
            if (currentRoom && data.room_id === currentRoom.id) {
                displayMessage(data);
            }
            break;

        case 'user_joined':
            if (currentRoom && data.room_id === currentRoom.id) {
                showSystemMessage(`${data.username} joined the room`);
                // Update member count
                updateMemberCount();
            }
            break;

        case 'user_left':
            if (currentRoom && data.room_id === currentRoom.id) {
                showSystemMessage(`${data.username} left the room`);
                updateMemberCount();
            }
            break;

        default:
            console.log('Unknown message type:', data.type);
    }
}

// ==================== API CALLS ====================
async function apiCall(endpoint, options = {}) {
    const defaultOptions = {
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${token}`
        }
    };

    const response = await fetch(`${API_URL}${endpoint}`, {
        ...defaultOptions,
        ...options,
        headers: {
            ...defaultOptions.headers,
            ...options.headers
        }
    });

    if (response.status === 401) {
        // Unauthorized - redirect to login
        logout();
        return null;
    }

    return response;
}

async function loadRooms() {
    try {
        const response = await apiCall(`/users/${userId}/rooms`);
        const data = await response.json();

        if (response.ok) {
            rooms = data.rooms || [];
            displayRooms();
        } else {
            showAlert('Failed to load rooms', 'error');
        }
    } catch (error) {
        console.error('Error loading rooms:', error);
        showAlert('Failed to load rooms', 'error');
    }
}

async function createRoom(name, description, type) {
    try {
        const response = await apiCall('/rooms', {
            method: 'POST',
            body: JSON.stringify({
                name,
                description,
                type
            })
        });

        const data = await response.json();

        if (response.ok) {
            showAlert('Room created successfully!', 'success');
            await loadRooms();
            // Creator is already a member, so just open the room
            await openRoom(data);
            return data;
        } else {
            showAlert(data.message || 'Failed to create room', 'error');
            return null;
        }
    } catch (error) {
        console.error('Error creating room:', error);
        showAlert('Failed to create room', 'error');
        return null;
    }
}

// Open a room (user is already a member)
async function openRoom(room) {
    currentRoom = room;

    // Update UI
    document.getElementById('welcomeScreen').style.display = 'none';
    document.getElementById('chatScreen').style.display = 'flex';
    document.getElementById('currentRoomName').textContent = room.name;
    document.getElementById('currentRoomType').textContent = room.type;
    updateMemberCount();

    // Load messages
    await loadMessages(room.id);

    // Join via WebSocket
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
            type: 'join_room',
            room_id: room.id
        }));
    }

    // Update active room in sidebar
    updateActiveRoom(room.id);
}

async function joinRoom(room) {
    try {
        // Join via API first
        const response = await apiCall(`/rooms/${room.id}/join`, {
            method: 'POST',
            body: JSON.stringify({
                user_id: parseInt(userId),
                room_id: room.id
            })
        });

        if (response.ok) {
            // Successfully joined, now open the room
            await openRoom(room);
        } else {
            const data = await response.json();
            // If already a member, just open the room
            if (data.message && data.message.includes('already')) {
                await openRoom(room);
            } else {
                showAlert(data.message || 'Failed to join room', 'error');
            }
        }
    } catch (error) {
        console.error('Error joining room:', error);
        showAlert('Failed to join room', 'error');
    }
}

async function leaveRoom() {
    if (!currentRoom) return;

    try {
        const response = await apiCall(`/rooms/${currentRoom.id}/leave`, {
            method: 'POST',
            body: JSON.stringify({
                user_id: parseInt(userId),
                room_id: currentRoom.id
            })
        });

        if (response.ok) {
            // Leave via WebSocket
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({
                    type: 'leave_room'
                }));
            }

            currentRoom = null;

            // Update UI
            document.getElementById('chatScreen').style.display = 'none';
            document.getElementById('welcomeScreen').style.display = 'flex';

            // Reload rooms to update membership
            await loadRooms();
        }
    } catch (error) {
        console.error('Error leaving room:', error);
        showAlert('Failed to leave room', 'error');
    }
}

async function loadMessages(roomId) {
    try {
        const response = await apiCall(`/rooms/${roomId}/messages?limit=50`);
        const data = await response.json();

        if (response.ok) {
            const messagesContainer = document.getElementById('messagesContainer');
            messagesContainer.innerHTML = '';

            if (data.messages && data.messages.length > 0) {
                // Reverse to show oldest first
                data.messages.reverse().forEach(msg => {
                    displayMessage({
                        message_id: msg.id,
                        user_id: msg.user_id,
                        username: msg.username,
                        content: msg.content,
                        created_at: msg.created_at
                    });
                });
            } else {
                messagesContainer.innerHTML = '<div class="loading">No messages yet. Start the conversation!</div>';
            }

            scrollToBottom();
        }
    } catch (error) {
        console.error('Error loading messages:', error);
    }
}

function sendMessage(content) {
    if (!currentRoom || !ws || ws.readyState !== WebSocket.OPEN) {
        showAlert('Not connected to chat', 'error');
        return;
    }

    ws.send(JSON.stringify({
        type: 'send_message',
        content: content
    }));
}

// ==================== UI FUNCTIONS ====================
function displayRooms() {
    const roomsList = document.getElementById('roomsList');

    if (rooms.length === 0) {
        roomsList.innerHTML = '<div class="loading">No rooms yet. Create one to get started!</div>';
        return;
    }

    roomsList.innerHTML = rooms.map(room => `
        <div class="room-item" onclick="openRoom(${JSON.stringify(room).replace(/"/g, '&quot;')})">
            <div class="room-item-name">${escapeHtml(room.name)}</div>
            <div class="room-item-meta">
                <span class="room-type-badge">${room.type}</span>
                <span>${room.member_count || 0} members</span>
            </div>
        </div>
    `).join('');
}

function updateActiveRoom(roomId) {
    document.querySelectorAll('.room-item').forEach((item, index) => {
        item.classList.remove('active');
        // Add active class to the matching room
        if (rooms[index] && rooms[index].id === roomId) {
            item.classList.add('active');
        }
    });
}

function displayMessage(msg) {
    const messagesContainer = document.getElementById('messagesContainer');
    const isOwnMessage = msg.user_id === parseInt(userId);

    const messageDiv = document.createElement('div');
    messageDiv.className = `message ${isOwnMessage ? 'own' : ''}`;

    const time = msg.created_at ? new Date(msg.created_at * 1000).toLocaleTimeString() : new Date().toLocaleTimeString();

    messageDiv.innerHTML = `
        <div class="message-header">
            <span class="message-author">${escapeHtml(msg.username)}</span>
            <span class="message-time">${time}</span>
        </div>
        <div class="message-content">${escapeHtml(msg.content)}</div>
    `;

    messagesContainer.appendChild(messageDiv);
    scrollToBottom();
}

function showSystemMessage(message) {
    const messagesContainer = document.getElementById('messagesContainer');
    const messageDiv = document.createElement('div');
    messageDiv.className = 'message system';
    messageDiv.innerHTML = `
        <div class="message-content">${escapeHtml(message)}</div>
    `;
    messagesContainer.appendChild(messageDiv);
    scrollToBottom();
}

function scrollToBottom() {
    const container = document.getElementById('messagesContainer');
    container.scrollTop = container.scrollHeight;
}

function updateMemberCount() {
    if (currentRoom) {
        // Could fetch updated room info from API
        document.getElementById('memberCount').textContent = `${currentRoom.member_count || 0} members`;
    }
}

// ==================== MODAL FUNCTIONS ====================
function showCreateRoomModal() {
    document.getElementById('createRoomModal').style.display = 'flex';
}

function closeCreateRoomModal() {
    document.getElementById('createRoomModal').style.display = 'none';
    document.getElementById('createRoomForm').reset();
}

// ==================== EVENT LISTENERS ====================
function setupEventListeners() {
    // Message form
    document.getElementById('messageForm').addEventListener('submit', (e) => {
        e.preventDefault();
        const input = document.getElementById('messageInput');
        const content = input.value.trim();

        if (content) {
            sendMessage(content);
            input.value = '';
        }
    });

    // Create room form
    document.getElementById('createRoomForm').addEventListener('submit', async (e) => {
        e.preventDefault();

        const name = document.getElementById('roomName').value;
        const description = document.getElementById('roomDescription').value;
        const type = document.getElementById('roomType').value;

        const room = await createRoom(name, description, type);

        if (room) {
            closeCreateRoomModal();
        }
    });

    // Close modal on background click
    document.getElementById('createRoomModal').addEventListener('click', (e) => {
        if (e.target.id === 'createRoomModal') {
            closeCreateRoomModal();
        }
    });
}

// ==================== UTILITY FUNCTIONS ====================
function showAlert(message, type = 'info') {
    const container = document.getElementById('alertContainer');
    const alert = document.createElement('div');
    alert.className = `alert alert-${type}`;
    alert.textContent = message;
    container.appendChild(alert);
    setTimeout(() => alert.remove(), 5000);
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function logout() {
    localStorage.clear();
    if (ws) {
        ws.close();
    }
    window.location.href = '/web/login.html';
}
