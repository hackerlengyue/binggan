(() => {
 const params = new URLSearchParams(location.search);
 const english = params.get('lang') === 'en';
 document.documentElement.lang = english ? 'en' : 'zh-CN';
 const video = document.getElementById('video');
 const error = document.getElementById('error');
 const loading = document.getElementById('loading');
 loading.textContent = english ? 'Loading video…' : '正在加载视频…';
 for (const event of ['loadeddata', 'playing', 'canplay']) video.addEventListener(event, () => { loading.hidden = true; });
 video.addEventListener('waiting', () => { loading.hidden = false; });
 const id = params.get('id');
 const fail = () => { loading.hidden = true; error.textContent = english ? 'Unable to play this video. Check the file or download it to play locally.' : '无法播放此视频，请检查文件或下载后在本地播放。'; error.hidden = false; };
 if (!id || !/^[a-zA-Z0-9-]{1,100}$/.test(id)) { fail(); return; }
 video.addEventListener('error', fail);
 const revision = params.get('v') || '';
 let desktop = !!window.mygo;
 try { desktop ||= window.parent !== window && !!window.parent.mygo; } catch {}
 const streamURL = path => desktop ? 'binggan-stream://localhost' + path : path;
 video.poster = streamURL('/api/resources/' + encodeURIComponent(id) + '/thumbnail?v=' + encodeURIComponent(revision));
 video.src = streamURL('/api/resources/' + encodeURIComponent(id) + '/stream');
 const labels = english ? {toggle:'Play / pause',rewind:'Back 10 seconds',forward:'Forward 10 seconds',mute:'Mute / unmute',fullscreen:'Fullscreen'} : {toggle:'播放 / 暂停',rewind:'后退 10 秒',forward:'前进 10 秒',mute:'静音 / 取消静音',fullscreen:'全屏'};
 window.AppleTVLikePlayer.enhance(video);
 document.addEventListener('keydown', event => {
  if (event.key !== 'Escape' || document.fullscreenElement || video.webkitDisplayingFullscreen) return;
  event.preventDefault();
  window.parent.postMessage({type:'bg-player-close'}, location.origin);
 });
 for (const [action, label] of Object.entries(labels)) {
  const button = document.querySelector('[data-act="' + action + '"]');
  button?.setAttribute('aria-label', label);
  button?.setAttribute('title', label);
 }
 document.querySelector('[data-el="vol"]')?.setAttribute('aria-label', english ? 'Volume' : '音量');
 window.addEventListener('pagehide', () => {video.pause(); video.removeAttribute('src'); video.load();});
})();
