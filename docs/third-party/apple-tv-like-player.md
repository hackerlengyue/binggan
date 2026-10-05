# Apple TV Like Player

Source: https://github.com/doraFX/apple-tv-like-player
Revision: 85027cef4d5eebf45c59d28b2fe5022cf5d16427
License: [MIT](../../frontend/public/player/LICENSE)

apple-video-player.css is unmodified upstream. apple-video-player.js includes
a local volume-control fix: changing the volume slider unmutes the video so a
newly selected level is preserved.
index.html, frame.js and frame.css integrate the player into Binggan with same-origin
video streaming, localized labels and errors. The iframe isolates upstream DOM
observers and document event listeners; closing it releases the player.
