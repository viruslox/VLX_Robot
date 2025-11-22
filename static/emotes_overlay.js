const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';
const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const host = window.location.host;

function connect() {
    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => console.log("Emote Wall Connected.");

    socket.onclose = () => {
        console.warn("Disconnected. Reconnecting...");
        setTimeout(connect, 3000);
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type === 'emote_wall' && data.emotes) {
                spawnEmotes(data.emotes);
            }
        } catch (e) {
            console.error(e);
        }
    };
}

function spawnEmotes(urls) {
    urls.forEach((url, index) => {
        // timerly spacing each emotes
        setTimeout(() => {
            createEmoteElement(url);
        }, index * 100);
    });
}

function createEmoteElement(url) {
    const img = document.createElement('img');
    img.src = url;
    img.classList.add('emote');

    // Random orizzontal position (0% - 90% width)
    const leftPos = Math.random() * 90;
    img.style.left = leftPos + 'vw';

    // Random animation time (4s to 8s)
    const duration = 4 + Math.random() * 4;
    img.style.animationDuration = duration + 's';

    // Random dimension
    const scale = 0.5 + Math.random() * 0.8; // ( 50% to 130%)
    img.style.transform = `scale(${scale})`;

    document.body.appendChild(img);

    // remove element after animation
    setTimeout(() => {
        img.remove();
    }, duration * 1000);
}

connect();
