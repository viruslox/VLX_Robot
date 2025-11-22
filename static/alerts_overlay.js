// --- Globals ---
const alertQueue = [];
let isAlertShowing = false;
let basePath = ''; // Base path for assets, derived from WebSocket path

// --- DOM Element Cache ---
const container = document.getElementById('alert-container');
const imageElement = document.getElementById('alert-image');
const videoElement = document.getElementById('alert-video');
const titleElement = document.getElementById('alert-title');
const detailElement = document.getElementById('alert-detail');
const messageElement = document.getElementById('alert-message');

// --- WebSocket Connection ---
function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';

    // Set the global base path for static assets /vlxrobot/ws -> /vlxrobot. If root, /ws -> ""
    basePath = wsPath.substring(0, wsPath.lastIndexOf('/'));

    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => {
        console.log("Overlay WebSocket connected.");
    };

    socket.onclose = (event) => {
        console.warn("Overlay disconnected. Reconnecting in 3 seconds...", event.reason);
        setTimeout(connect, 3000);
    };

    socket.onerror = (error) => {
        console.error("WebSocket Error: ", error);
        socket.close();
    };

    // --- Message Handler ---
    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            alertQueue.push(data);
            processQueue();
        } catch (err) {
            console.error("Failed to parse message:", event.data, err);
        }
    };
}

// --- Queue Handler ---
function processQueue() {
    if (isAlertShowing || alertQueue.length === 0) {
        return;
    }

    isAlertShowing = true;
    const data = alertQueue.shift();

    // --- Alert Configuration ---
    let config = {
        duration: 8000,
        sound: `${basePath}/static/alerts/alert.mp3`
    };

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
            config.duration = data.message ? 8000 : 5000;
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
            config.duration = 8000;
            break;

        case 'twitch_raid':
            config.title = "Incoming Raid!";
            config.detail = `${data.raider_name} is raiding with ${data.viewers} viewers!`;
            config.image = `${basePath}/static/alerts/raid.mp4`;
            config.duration = 10000;
            break;

        case 'youtube_member':
            config.title = "New Member";
            config.detail = data.user_name;
            config.image = `${basePath}/static/alerts/follow.mp4`;
//            config.sound = `${basePath}/static/alerts/follow.mp3`;
            break;

        case 'youtube_super_chat':
            config.title = `Super Chat: ${data.amount_string}`;
            config.detail = data.user_name;
            config.message = data.message || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
//            config.sound = `${basePath}/static/alerts/superchat.mp3`;
            config.duration = 8000;
            break;

        case 'youtube_super_sticker':
            config.title = `Super Sticker: ${data.amount_string}`;
            config.detail = data.user_name;
            config.message = data.sticker_alt || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            config.du

        case 'stream_tip':
            config.title = "New Donation!";
            config.detail = `${data.user_name} (${data.amount_string})`;
            config.message = data.message || "";
            config.image = `${basePath}/static/alerts/cheer.mp4`;
            config.duration = 8000;
            break;

        default:
            console.warn("Unhandled event type:", data.type);
            isAlertShowing = false;
            processQueue(); // Try the next item
            return;
    }

    showAlert(config);
}

// --- Alert Display ---
function showAlert(config) {
    // 1. Play sound (if configured)
    if (config.sound) {
        const audio = new Audio(config.sound);
        audio.play().catch(e => console.warn("Audio playback failed:", e.message));
    }

    // 2. Set content
    imageElement.style.display = 'none';
    videoElement.style.display = 'none';
    videoElement.pause();
    videoElement.src = '';
    imageElement.src = '';
 // 'config.image' has the image or the video
    const mediaUrl = config.image || '';

    if (mediaUrl.endsWith('.mp4') || mediaUrl.endsWith('.webm')) {
        videoElement.src = mediaUrl;
        videoElement.style.display = 'block';
        videoElement.load();
        videoElement.play().catch(e => console.warn("Video playback failed:", e.message));
    }
    else if (mediaUrl) {
        imageElement.src = mediaUrl;
        imageElement.style.display = 'block';
    }

    titleElement.innerText = config.title || '';
    detailElement.innerText = config.detail || '';

    if (config.message) {
        messageElement.innerText = config.message;
        messageElement.style.display = 'block';
    } else {
        messageElement.style.display = 'none';
    }

    // 3. Show the alert
    container.classList.remove('hidden');

    // 4. Set timer to hide alert
    setTimeout(() => {
        container.classList.add('hidden');

        // Wait for CSS fade-out (e.g., 500ms) before processing next alert
        setTimeout(() => {
            isAlertShowing = false;
            processQueue();
        }, 500); // Must match the CSS transition duration

    }, config.duration);
}

// --- Start Connection ---
connect();
