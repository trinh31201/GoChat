// Configuration - Dynamic URL for phone/deployment
const API_HOST = window.location.hostname;
const API_PORT = window.location.port;
// If port is empty (default 80), don't add :port to URL
const API_URL = API_PORT ? `http://${API_HOST}:${API_PORT}/api/v1` : `http://${API_HOST}/api/v1`;
const WS_URL = API_PORT ? `ws://${API_HOST}:${API_PORT}/ws` : `ws://${API_HOST}/ws`;

// Global State
let ws = null;
let wsAuthenticated = false;  // Track if WebSocket is authenticated
let currentRoom = null;
let pendingRoomJoin = null;   // Room to join after auth completes
let rooms = [];
let token = localStorage.getItem('token');
let userId = localStorage.getItem('userId');
let username = localStorage.getItem('username');
let selectedFile = null;      // Currently selected file for upload

// Check authentication
if (!token || !userId || !username) {
    window.location.href = '/web/login.html';
}

// Validate token matches userId by decoding JWT payload
function validateTokenUserId() {
    try {
        // JWT format: header.payload.signature
        const payload = JSON.parse(atob(token.split('.')[1]));
        const tokenUserId = String(payload.user_id);

        // If token's user_id doesn't match localStorage userId, force re-login
        if (tokenUserId !== String(userId)) {
            console.warn('Token userId mismatch! Forcing re-login.');
            localStorage.clear();
            window.location.href = '/web/login.html';
        }
    } catch (e) {
        console.error('Invalid token, forcing re-login');
        localStorage.clear();
        window.location.href = '/web/login.html';
    }
}

// Run validation on page load
validateTokenUserId();

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
        wsAuthenticated = false;  // Reset auth state
        // Reconnect after 3 seconds
        setTimeout(initializeWebSocket, 3000);
    };
}

function handleWebSocketMessage(data) {
    console.log('WebSocket message:', data);

    switch (data.type) {
        case 'success':
            console.log(data.message);
            // Check if this is authentication success
            if (data.message && data.message.includes('Authenticated')) {
                wsAuthenticated = true;
                // If there's a pending room to join, join it now
                if (pendingRoomJoin) {
                    joinRoomWS(pendingRoomJoin);
                    pendingRoomJoin = null;
                }
            }
            break;

        case 'error':
            console.log(data.message);
            break;

        case 'room_joined':
            console.log('Successfully joined room', data.room_id);
            break;

        case 'new_message':
            // Use == for loose comparison (handles string vs number)
            if (currentRoom && String(data.room_id) === String(currentRoom.id)) {
                displayMessage(data);
            }
            break;

        case 'user_joined':
            if (currentRoom && String(data.room_id) === String(currentRoom.id)) {
                showSystemMessage(`${data.username} joined the room`);
            }
            break;

        case 'user_left':
            if (currentRoom && String(data.room_id) === String(currentRoom.id)) {
                showSystemMessage(`${data.username} left the room`);
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

// Send join_room message via WebSocket
function joinRoomWS(roomId) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        console.log('Sending join_room for room:', roomId);
        ws.send(JSON.stringify({
            type: 'join_room',
            room_id: parseInt(roomId)  // Ensure it's a number
        }));
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

    // Load messages
    await loadMessages(room.id);

    // Join via WebSocket (wait for auth if not authenticated yet)
    if (wsAuthenticated) {
        joinRoomWS(room.id);
    } else {
        // Store room to join after auth completes
        pendingRoomJoin = room.id;
        console.log('Waiting for WebSocket auth before joining room');
    }

    // Update active room in sidebar
    updateActiveRoom(room.id);
}

// Join room by ID (from input field)
async function joinRoomById() {
    const roomIdInput = document.getElementById('joinRoomId');
    const roomId = roomIdInput.value.trim();

    if (!roomId) {
        showAlert('Please enter a room ID', 'error');
        return;
    }

    try {
        // Try to join the room first (user may not have access to view room details yet)
        const joinResponse = await apiCall(`/rooms/${roomId}/join`, {
            method: 'POST',
            body: JSON.stringify({
                user_id: parseInt(userId),
                room_id: parseInt(roomId)
            })
        });

        if (!joinResponse.ok) {
            const errorData = await joinResponse.json();
            // If already a member, that's fine - just proceed to open the room
            if (!errorData.message || !errorData.message.includes('already')) {
                showAlert(errorData.message || 'Failed to join room', 'error');
                return;
            }
        }

        // Now fetch room details (user is now a member)
        const roomResponse = await apiCall(`/rooms/${roomId}`);
        if (!roomResponse.ok) {
            showAlert('Failed to load room details', 'error');
            return;
        }
        const room = await roomResponse.json();

        // Clear input
        roomIdInput.value = '';

        // Reload rooms list and open the room
        await loadRooms();
        await openRoom(room);

        showAlert('Joined room successfully!', 'success');
    } catch (error) {
        console.error('Error joining room:', error);
        showAlert('Failed to join room', 'error');
    }
}

async function joinRoom(room) {
    try {
        // Join via API first
        const response = await apiCall(`/rooms/${room.id}/join`, {
            method: 'POST',
            body: JSON.stringify({
                user_id: parseInt(userId),
                room_id: parseInt(room.id)
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
                        created_at: msg.created_at,
                        message_type: msg.file_url ? (msg.mime_type?.startsWith('image/') ? 'image' : 'file') : 'text',
                        file_url: msg.file_url,
                        file_name: msg.file_name,
                        file_size: msg.file_size,
                        mime_type: msg.mime_type
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

function sendMessage(content, fileData = null) {
    if (!currentRoom || !ws || ws.readyState !== WebSocket.OPEN) {
        showAlert('Not connected to chat', 'error');
        return;
    }

    const message = {
        type: 'send_message',
        content: content
    };

    // Add file data if present
    if (fileData) {
        message.message_type = fileData.message_type;
        message.file_url = fileData.file_url;
        message.file_name = fileData.file_name;
        message.file_size = fileData.file_size;
        message.mime_type = fileData.mime_type;
    }

    ws.send(JSON.stringify(message));
}

// Upload file to server
async function uploadFile(file) {
    const formData = new FormData();
    formData.append('file', file);

    try {
        const response = await fetch(`${API_URL}/upload`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`
            },
            body: formData
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Upload failed');
        }

        return await response.json();
    } catch (error) {
        console.error('Upload error:', error);
        throw error;
    }
}

// Handle file selection
function handleFileSelect(event) {
    const file = event.target.files[0];
    if (!file) return;

    // Check file size (max 10MB)
    if (file.size > 10 * 1024 * 1024) {
        showAlert('File too large. Maximum size is 10MB.', 'error');
        event.target.value = '';
        return;
    }

    selectedFile = file;
    showFilePreview(file);
}

// Show file preview
function showFilePreview(file) {
    const preview = document.getElementById('filePreview');
    const imagePreview = document.getElementById('imagePreview');
    const fileInfo = document.getElementById('fileInfo');
    const fileName = document.getElementById('fileName');
    const fileSize = document.getElementById('fileSize');

    preview.style.display = 'flex';

    if (file.type.startsWith('image/')) {
        // Show image preview
        const reader = new FileReader();
        reader.onload = (e) => {
            imagePreview.src = e.target.result;
            imagePreview.style.display = 'block';
        };
        reader.readAsDataURL(file);
        fileInfo.style.display = 'none';
    } else {
        // Show file info
        imagePreview.style.display = 'none';
        fileInfo.style.display = 'flex';
        fileName.textContent = file.name;
        fileSize.textContent = formatFileSize(file.size);
    }
}

// Remove selected file
function removeSelectedFile() {
    selectedFile = null;
    document.getElementById('fileInput').value = '';
    document.getElementById('filePreview').style.display = 'none';
    document.getElementById('imagePreview').style.display = 'none';
    document.getElementById('fileInfo').style.display = 'none';
}

// Format file size
function formatFileSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
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

    // Build message content based on type
    let contentHtml = '';

    if (msg.message_type === 'image' && msg.file_url) {
        // Image message
        contentHtml = `
            <div class="message-image">
                <a href="${escapeHtml(msg.file_url)}" target="_blank">
                    <img src="${escapeHtml(msg.file_url)}" alt="${escapeHtml(msg.file_name || 'Image')}" loading="lazy">
                </a>
            </div>
        `;
        if (msg.content) {
            contentHtml += `<div class="message-text">${escapeHtml(msg.content)}</div>`;
        }
    } else if (msg.message_type === 'file' && msg.file_url) {
        // File message
        contentHtml = `
            <div class="message-file">
                <a href="${escapeHtml(msg.file_url)}" target="_blank" download="${escapeHtml(msg.file_name || 'file')}">
                    <span class="file-icon">📎</span>
                    <span class="file-name">${escapeHtml(msg.file_name || 'Download file')}</span>
                    <span class="file-size">${msg.file_size ? formatFileSize(msg.file_size) : ''}</span>
                </a>
            </div>
        `;
        if (msg.content) {
            contentHtml += `<div class="message-text">${escapeHtml(msg.content)}</div>`;
        }
    } else {
        // Text message
        contentHtml = escapeHtml(msg.content);
    }

    messageDiv.innerHTML = `
        <div class="message-header">
            <span class="message-author">${escapeHtml(msg.username)}</span>
            <span class="message-time">${time}</span>
        </div>
        <div class="message-content">${contentHtml}</div>
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
    // File input change
    document.getElementById('fileInput').addEventListener('change', handleFileSelect);

    // Message form
    document.getElementById('messageForm').addEventListener('submit', async (e) => {
        e.preventDefault();
        const input = document.getElementById('messageInput');
        const content = input.value.trim();

        // If there's a file selected, upload it first
        if (selectedFile) {
            try {
                showAlert('Uploading file...', 'info');
                const uploadResult = await uploadFile(selectedFile);

                // Send message with file data
                sendMessage(content || '', uploadResult);

                // Clear file selection
                removeSelectedFile();
                input.value = '';
            } catch (error) {
                showAlert('Failed to upload file: ' + error.message, 'error');
            }
            return;
        }

        // Text-only message
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
