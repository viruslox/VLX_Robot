// --- Global State & Configuration ---
const alertQueue = [];
let isAlertShowing = false;
let basePath = '';

// Calculate master volume (normalized 0.0 - 1.0), defaulting to 1.0 if undefined
const masterVolume = (window.VLX_CONFIG && typeof window.VLX_CONFIG.VOLUME === 'number') 
    ? (window.VLX_CONFIG.VOLUME / 100) 
    : 1.0;

// --- DOM Elements ---
const container = document.getElementById('alert-container');
const imageElement = document.getElementById('alert-image');
const videoElement = document.getElementById('alert-video');
const titleElement = document.getElementById('alert-title');
const detailElement = document.getElementById('alert-detail');
const messageElement = document.getElementById('alert-message');

// --- WebSocket Connection Management ---
function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';
    
    // Derive base path from WebSocket path (e.g., /vlxrobot/ws -> /vlxrobot)
    basePath = wsPath.substring(0, wsPath.lastIndexOf('/'));

    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => console.log("[System] Alert Overlay Connected");

    socket.onclose = (event) => {
        console.warn(`[System] Connection lost (Code: ${event.code}). Reconnecting in 3s...`);
        setTimeout(connect, 3000);
    };

    socket.onerror = (error) => {
        console.error("[Error] WebSocket error:", error);
        socket.close();
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            alertQueue.push(data);
            processQueue();
        } catch (err) {
            console.error("[Error] Failed to parse payload:", err);
        }
    };
}

// --- Queue Processing ---
function processQueue() {
    if (isAlertShowing || alertQueue.length === 0) return;

    isAlertShowing = true;
    const data = alertQueue.shift();

    // Default configuration
    let config = {
        duration: 8000,
        sound: `${basePath}/static/alerts/alert.mp3`,
        volume: masterVolume // Apply configured volume
    };

    // Map Event Types to Visual Assets
    switch (data.type) {
        case 'twitch_follow':
            config.title = "New Follower";
            config.detail = data.user_name;
            config.image = `${basePath}/static/alerts/follow.mp4`;
            break;

        case 'twitch_subscribe':
            config.title = `New Tier ${data.tier.charAt(0)} Sub!`;
            config.detail = data.user_name;
            config.image = `${basePath}/static/alerts/sub.mp4`;
            break;

        case 'twitch_resubscribe':
            config.title = `${data.cumulative_months || data.months} Month Resub!`;
            config.detail = data.user_name;
            config.message = data.message;
            config.image = `${basePath}/static/alerts/sub.mp4`;
            config.duration = data.message ? 8000 : 6000;
            break;

        case 'twitch_gift_sub':
            config.detail = data.is_anonymous ? "An Anonymous Gifter" : data.gifter_name;
            config.title = `Gifted ${data.total_gifts} Tier ${data.tier.charAt(0)} Sub(s)!`;
            config.image = `${basePath}/static/alerts/sub.mp4`;
            config.duration = 8000;
            break;

        case 'twitch_cheer':
            config.title = `${data.bits} Bit Cheer!`;
            config.detail = data.is_anonymous ? "Anonymous" : data.user_name;
            config.message = data.message;
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            break;

        case 'twitch_raid':
            config.title = "Incoming Raid!";
            config.detail = `${data.raider_name} raiding with ${data.viewers} viewers!`;
            config.image = `${basePath}/static/alerts/raid.mp4`;
            config.duration = 10000;
            break;

        case 'youtube_member':
            config.title = "New Member";
            config.detail = data.user_name;
            config.image = `${basePath}/static/alerts/follow.mp4`;
            break;

        case 'youtube_super_chat':
            config.title = `Super Chat: ${data.amount_string}`;
            config.detail = data.user_name;
            config.message = data.message || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            break;

        case 'youtube_super_sticker':
            config.title = `Super Sticker: ${data.amount_string}`;
            config.detail = data.user_name;
            config.message = data.sticker_alt || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            break;

        case 'stream_tip':
            config.title = "New Donation!";
            config.detail = `${data.user_name} (${data.amount_string})`;
            config.message = data.message || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            break;

        default:
            console.warn("[Warn] Unhandled event type:", data.type);
            isAlertShowing = false;
            processQueue();
            return;
    }

    showAlert(config);
}

// --- Alert Rendering ---
function showAlert(config) {
    // 1. Audio Playback
    if (config.sound) {
        const audio = new Audio(config.sound);
        audio.volume = config.volume;
        audio.play().catch(e => console.warn("[Warn] Audio playback failed:", e.message));
    }

    // 2. Reset DOM State
    imageElement.style.display = 'none';
    videoElement.style.display = 'none';
    videoElement.pause();
    videoElement.src = '';
    imageElement.src = '';

    const mediaUrl = config.image || '';

    // 3. Media Rendering (Video vs Image)
    if (mediaUrl.endsWith('.mp4') || mediaUrl.endsWith('.webm')) {
        videoElement.src = mediaUrl;
        videoElement.volume = config.volume; // Apply volume to video track
        videoElement.style.display = 'block';
        videoElement.load();
        videoElement.play().catch(e => console.warn("[Warn] Video playback failed:", e.message));
    } else if (mediaUrl) {
        imageElement.src = mediaUrl;
        imageElement.style.display = 'block';
    }

    // 4. Text Content
    titleElement.innerText = config.title || '';
    detailElement.innerText = config.detail || '';

    if (config.message) {
        messageElement.innerText = config.message;
        messageElement.style.display = 'block';
    } else {
        messageElement.style.display = 'none';
    }

    // 5. Animation / Visibility Cycle
    container.classList.remove('hidden');

    setTimeout(() => {
        container.classList.add('hidden');
        
        // Wait for CSS transition (500ms) before processing next alert
        setTimeout(() => {
            isAlertShowing = false;
            processQueue();
        }, 500);

    }, config.duration);
}

// --- Initialization ---
connect();
