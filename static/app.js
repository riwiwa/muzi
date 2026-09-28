// muzi — the one script for every page. No dependencies.
(function () {
  'use strict';

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  function isTyping(el) {
    return el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);
  }

  function request(method, url, body) {
    return fetch(url, {
      method: method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined
    }).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { throw new Error(t || res.statusText); });
      return res;
    });
  }

  // ---------- command palette ----------

  var palette = $('#palette');
  var paletteInput = $('#paletteInput');
  var paletteResults = $('#paletteResults');
  var active = -1;
  var searchTimer = null;
  var searchSeq = 0;

  function openPalette() {
    if (!palette) return;
    palette.hidden = false;
    paletteInput.value = '';
    renderResults(null);
    paletteInput.focus();
  }

  function closePalette() {
    if (palette && !palette.hidden) palette.hidden = true;
  }

  function setActive(i) {
    var items = $$('a', paletteResults);
    if (!items.length) return;
    active = (i + items.length) % items.length;
    items.forEach(function (a, n) {
      a.classList.toggle('active', n === active);
      a.setAttribute('aria-selected', n === active ? 'true' : 'false');
    });
    items[active].scrollIntoView({ block: 'nearest' });
  }

  // built with DOM APIs, never innerHTML, since names are user data
  function renderResults(results) {
    paletteResults.textContent = '';
    active = -1;
    if (results === null) return;
    if (!results.length) {
      var empty = document.createElement('li');
      empty.className = 'palette-empty';
      empty.textContent = 'Nothing matches that.';
      paletteResults.appendChild(empty);
      return;
    }
    results.slice(0, 12).forEach(function (r) {
      var li = document.createElement('li');
      var a = document.createElement('a');
      a.href = r.url;
      a.setAttribute('role', 'option');

      var type = document.createElement('span');
      type.className = 'type';
      type.textContent = r.type;

      var name = document.createElement('span');
      name.className = 'name';
      name.textContent = r.name;
      if (r.artist) {
        var by = document.createElement('small');
        by.textContent = r.artist;
        name.appendChild(by);
      }

      var count = document.createElement('span');
      count.className = 'count';
      count.textContent = Number(r.count).toLocaleString() + ' plays';

      a.appendChild(type);
      a.appendChild(name);
      a.appendChild(count);
      a.addEventListener('mousemove', function () {
        setActive($$('a', paletteResults).indexOf(a));
      });
      li.appendChild(a);
      paletteResults.appendChild(li);
    });
    setActive(0);
  }

  if (palette) {
    var trigger = $('#searchTrigger');
    if (trigger) trigger.addEventListener('click', openPalette);

    palette.addEventListener('mousedown', function (e) {
      if (e.target === palette) closePalette();
    });

    paletteInput.addEventListener('input', function () {
      var q = paletteInput.value.trim();
      clearTimeout(searchTimer);
      if (!q) { renderResults(null); return; }
      searchTimer = setTimeout(function () {
        var seq = ++searchSeq;
        fetch('/search?q=' + encodeURIComponent(q))
          .then(function (res) { return res.ok ? res.json() : []; })
          .then(function (results) {
            if (seq === searchSeq) renderResults(results || []);
          })
          .catch(function () {});
      }, 120);
    });

    paletteInput.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setActive(active + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setActive(active - 1); }
      else if (e.key === 'Enter') {
        var items = $$('a', paletteResults);
        if (items[active]) { e.preventDefault(); window.location.href = items[active].href; }
      }
    });
  }

  // ---------- global keys ----------

  document.addEventListener('keydown', function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      if (palette && palette.hidden) openPalette(); else closePalette();
    } else if (e.key === '/' && !isTyping(e.target)) {
      e.preventDefault();
      openPalette();
    } else if (e.key === 'Escape') {
      closePalette();
      window.closeEditModal();
      $$('details.range[open]').forEach(function (d) { d.open = false; });
    }
  });

  // ---------- edit modal ----------

  window.openEditModal = function () {
    var modal = $('#editModal');
    if (!modal) return;
    modal.style.display = 'flex';
    var first = $('input, textarea', modal);
    if (first) first.focus();
  };

  window.closeEditModal = function () {
    var modal = $('#editModal');
    if (modal) modal.style.display = 'none';
  };

  var editModal = $('#editModal');
  if (editModal) {
    editModal.addEventListener('mousedown', function (e) {
      if (e.target === editModal) window.closeEditModal();
    });
  }

  var editForm = $('#editForm');
  if (editForm) {
    editForm.addEventListener('submit', function (e) {
      e.preventDefault();
      var entity = editForm.dataset.entity;
      var data = {};
      $$('input, textarea', editForm).forEach(function (el) {
        if (el.name) data[el.name] = el.value;
      });
      request('PATCH', '/api/' + entity + '/' + editForm.dataset.id + '/batch', data)
        .then(function (res) { return res.json(); })
        .then(function (r) {
          // a renamed song lives at a new URL
          if (entity === 'song' && r.artist && r.title && r.username) {
            window.location.href = '/profile/' + r.username + '/song/' +
              encodeURIComponent(r.artist) + '/' + encodeURIComponent(r.title);
          } else {
            window.location.reload();
          }
        })
        .catch(function (err) { alert('Could not save: ' + err.message); });
    });
  }

  // clears a custom image so it's fetched automatically again
  window.resetImage = function (entity, id, field) {
    request('PATCH', '/api/' + entity + '/' + id + '/edit?field=' + field, { value: '' })
      .then(function () { window.location.reload(); })
      .catch(function (err) { alert('Could not reset image: ' + err.message); });
  };

  // ---------- image upload (owner only; the hero art is marked .editable-image) ----------

  function pickImage(target) {
    var input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/jpeg,image/png,image/gif,image/webp';
    input.onchange = function () {
      var file = input.files[0];
      if (!file) return;
      if (file.size > 5 * 1024 * 1024) { alert('Images must be under 5MB.'); return; }

      var form = new FormData();
      form.append('file', file);
      target.style.opacity = '0.5';
      fetch('/api/upload/image', { method: 'POST', body: form })
        .then(function (res) {
          if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
          return res.json();
        })
        .then(function (uploaded) {
          var url = '/api/' + target.dataset.entity + '/' + target.dataset.id + '/edit?field=' + target.dataset.field;
          return request('PATCH', url, { value: uploaded.url }).then(function () { return uploaded.url; });
        })
        .then(function (src) {
          var img = document.createElement('img');
          img.src = src;
          img.alt = '';
          target.textContent = '';
          target.appendChild(img);
        })
        .catch(function (err) { alert('Could not update image: ' + err.message); })
        .finally(function () { target.style.opacity = ''; });
    };
    input.click();
  }

  $$('.editable-image').forEach(function (el) {
    el.addEventListener('click', function () { pickImage(el); });
    el.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pickImage(el); }
    });
  });

  // ---------- removing plays (song page) ----------

  window.toggleRemoveMode = function () {
    document.body.classList.add('removing');
    $('#removeScrobblesBtn').style.display = 'none';
    $('#removeControls').style.display = 'contents';
  };

  window.cancelRemoveMode = function () {
    document.body.classList.remove('removing');
    $('#removeScrobblesBtn').style.display = '';
    $('#removeControls').style.display = 'none';
    $$('.scrobble-checkbox').forEach(function (cb) { cb.checked = false; });
  };

  window.deleteSelectedScrobbles = function () {
    var ids = $$('.scrobble-checkbox:checked').map(function (cb) { return parseInt(cb.value, 10); });
    if (!ids.length) { alert('Select at least one play to delete.'); return; }
    if (!confirm('Delete ' + ids.length + ' play' + (ids.length === 1 ? '' : 's') + '? This cannot be undone.')) return;
    request('POST', '/api/scrobble/delete', ids)
      .then(function () { window.location.reload(); })
      .catch(function (err) { alert('Could not delete: ' + err.message); });
  };

  // ---------- custom date ranges on profile charts ----------

  $$('.range-form').forEach(function (form) {
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var prefix = form.dataset.prefix;
      var params = new URLSearchParams(window.location.search);
      params.set(prefix + 'period', 'custom');
      ['start', 'end'].forEach(function (key) {
        var value = form.elements[key].value;
        if (value) params.set(prefix + key, value); else params.delete(prefix + key);
      });
      params.delete('page');
      window.location.href = '?' + params.toString() + '#' + form.dataset.anchor;
    });
  });

  // prefill custom ranges from the URL
  var query = new URLSearchParams(window.location.search);
  $$('.range-form').forEach(function (form) {
    ['start', 'end'].forEach(function (key) {
      var value = query.get(form.dataset.prefix + key);
      if (value) form.elements[key].value = value;
    });
  });

  // ---------- heatmap starts scrolled to today on narrow screens ----------

  $$('[data-scroll-end]').forEach(function (el) { el.scrollLeft = el.scrollWidth; });

  // ---------- move and crop before uploading a profile picture ----------

  var cropper = $('#cropper');
  if (cropper) {
    document.documentElement.classList.add('js');

    var OUTPUT_SIZE = 512;
    var stage = $('#cropStage');
    var cropImg = $('#cropImage');
    var zoomInput = $('#cropZoom');
    var cropError = $('#cropError');
    var saveBtn = $('#cropSave');
    var fileInput = null;
    var objectUrl = null;
    // x/y: image's top-left corner relative to the stage, in screen px; scale: screen px per image px
    var st = { x: 0, y: 0, scale: 1, base: 1, w: 0, h: 0 };

    function stageSize() { return stage.clientWidth; }

    // keep the image covering the whole square
    function clamp() {
      var size = stageSize();
      st.x = Math.min(0, Math.max(size - st.w * st.scale, st.x));
      st.y = Math.min(0, Math.max(size - st.h * st.scale, st.y));
    }

    function render() {
      clamp();
      cropImg.style.transform = 'translate(' + st.x + 'px,' + st.y + 'px) scale(' + st.scale + ')';
    }

    // zoom around a point on the stage (defaults to its center)
    function setZoom(zoom, px, py) {
      var size = stageSize();
      zoom = Math.min(Number(zoomInput.max), Math.max(1, zoom));
      if (px === undefined) { px = size / 2; py = size / 2; }
      var ix = (px - st.x) / st.scale;
      var iy = (py - st.y) / st.scale;
      st.scale = st.base * zoom;
      st.x = px - ix * st.scale;
      st.y = py - iy * st.scale;
      zoomInput.value = zoom;
      render();
    }

    function zoomLevel() { return st.scale / st.base; }

    function openCropper(input) {
      var file = input.files[0];
      if (!file) return;
      if (file.type.indexOf('image/') !== 0) { alert('Please choose an image.'); input.value = ''; return; }
      fileInput = input;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
      objectUrl = URL.createObjectURL(file);
      cropError.textContent = '';
      cropImg.onload = function () {
        cropper.hidden = false;
        var size = stageSize();
        st.w = cropImg.naturalWidth;
        st.h = cropImg.naturalHeight;
        cropImg.style.width = st.w + 'px';
        cropImg.style.height = st.h + 'px';
        st.base = size / Math.min(st.w, st.h);
        st.scale = st.base;
        st.x = (size - st.w * st.scale) / 2;
        st.y = (size - st.h * st.scale) / 2;
        zoomInput.value = 1;
        render();
        stage.focus();
      };
      cropImg.onerror = function () { alert('That image could not be read.'); closeCropper(); };
      cropImg.src = objectUrl;
    }

    function closeCropper() {
      cropper.hidden = true;
      if (fileInput) fileInput.value = '';
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save picture';
    }

    $$('input[type="file"][data-crop]').forEach(function (input) {
      input.addEventListener('change', function () { openCropper(input); });
    });

    // drag with mouse, touch or pen; two fingers pinch to zoom
    var pointers = {};
    var pinch = null;
    stage.addEventListener('pointerdown', function (e) {
      stage.setPointerCapture(e.pointerId);
      pointers[e.pointerId] = { x: e.clientX, y: e.clientY };
    });
    stage.addEventListener('pointermove', function (e) {
      var prev = pointers[e.pointerId];
      if (!prev) return;
      var ids = Object.keys(pointers);
      if (ids.length === 2) {
        var other = pointers[ids[0] == e.pointerId ? ids[1] : ids[0]];
        var dist = Math.hypot(e.clientX - other.x, e.clientY - other.y);
        var rect = stage.getBoundingClientRect();
        var mx = (e.clientX + other.x) / 2 - rect.left;
        var my = (e.clientY + other.y) / 2 - rect.top;
        if (pinch) setZoom(zoomLevel() * dist / pinch, mx, my);
        pinch = dist;
      } else {
        st.x += e.clientX - prev.x;
        st.y += e.clientY - prev.y;
        render();
      }
      pointers[e.pointerId] = { x: e.clientX, y: e.clientY };
    });
    function endPointer(e) {
      delete pointers[e.pointerId];
      pinch = null;
    }
    stage.addEventListener('pointerup', endPointer);
    stage.addEventListener('pointercancel', endPointer);

    stage.addEventListener('wheel', function (e) {
      e.preventDefault();
      var rect = stage.getBoundingClientRect();
      setZoom(zoomLevel() * Math.exp(-e.deltaY * 0.0015), e.clientX - rect.left, e.clientY - rect.top);
    }, { passive: false });

    zoomInput.addEventListener('input', function () { setZoom(Number(zoomInput.value)); });

    stage.addEventListener('keydown', function (e) {
      var step = e.shiftKey ? 40 : 10;
      if (e.key === 'ArrowLeft') st.x += step;
      else if (e.key === 'ArrowRight') st.x -= step;
      else if (e.key === 'ArrowUp') st.y += step;
      else if (e.key === 'ArrowDown') st.y -= step;
      else if (e.key === '+' || e.key === '=') { setZoom(zoomLevel() * 1.1); e.preventDefault(); return; }
      else if (e.key === '-') { setZoom(zoomLevel() / 1.1); e.preventDefault(); return; }
      else return;
      e.preventDefault();
      render();
    });

    cropper.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') { e.stopPropagation(); closeCropper(); }
    });
    cropper.addEventListener('mousedown', function (e) { if (e.target === cropper) closeCropper(); });
    $('#cropCancel').addEventListener('click', closeCropper);

    saveBtn.addEventListener('click', function () {
      var size = stageSize();
      var canvas = document.createElement('canvas');
      canvas.width = canvas.height = OUTPUT_SIZE;
      canvas.getContext('2d').drawImage(cropImg,
        -st.x / st.scale, -st.y / st.scale, size / st.scale, size / st.scale,
        0, 0, OUTPUT_SIZE, OUTPUT_SIZE);

      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving…';
      // WebP where supported; browsers without it fall back to PNG, which the server also accepts
      canvas.toBlob(function (blob) {
        if (!blob) { cropError.textContent = 'Could not crop that image.'; saveBtn.disabled = false; return; }
        var form = new FormData();
        form.append('file', blob, blob.type === 'image/webp' ? 'avatar.webp' : 'avatar.png');
        fetch(fileInput.form.action, { method: 'POST', body: form })
          .then(function (res) {
            if (!res.ok) throw new Error(res.statusText);
            // the server redirects back to settings, with pfp_error set if it rejected the image
            window.location.href = res.url;
          })
          .catch(function (err) {
            cropError.textContent = 'Upload failed: ' + err.message;
            saveBtn.disabled = false;
            saveBtn.textContent = 'Save picture';
          });
      }, 'image/webp', 0.9);
    });
  }

  // ---------- grid maker: save or copy the collage as a PNG ----------

  var collage = $('#collage');
  if (collage) {
    var gridStatus = $('#gridStatus');
    var gridButtons = [$('#gridSave'), $('#gridCopy')];
    var SERIF = '"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif';
    var SANS = 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

    function setGridStatus(text, kind) {
      gridStatus.textContent = text;
      gridStatus.className = 'grid-status' + (kind ? ' ' + kind : '');
    }

    function busy(on) {
      gridButtons.forEach(function (b) { if (b) b.disabled = on; });
    }

    function whenLoaded(img) {
      if (img.complete) return Promise.resolve();
      return new Promise(function (resolve) {
        img.addEventListener('load', resolve, { once: true });
        img.addEventListener('error', resolve, { once: true });
      });
    }

    // shortens text with an ellipsis to fit a width
    function fit(ctx, text, width) {
      if (ctx.measureText(text).width <= width) return text;
      while (text.length > 1 && ctx.measureText(text + '…').width > width) text = text.slice(0, -1);
      return text + '…';
    }

    function drawCell(ctx, cell, x, y, tile, names) {
      if (cell.classList.contains('collage-empty')) {
        ctx.fillStyle = '#090a08';
        ctx.fillRect(x, y, tile, tile);
        return;
      }
      var img = cell.querySelector('img');
      if (img && img.naturalWidth) {
        // crop to a centered square, like object-fit: cover
        var side = Math.min(img.naturalWidth, img.naturalHeight);
        ctx.drawImage(img, (img.naturalWidth - side) / 2, (img.naturalHeight - side) / 2, side, side, x, y, tile, tile);
      } else {
        var h = cell.dataset.hue;
        var bg = ctx.createRadialGradient(x + tile * 0.2, y + tile * 0.1, 0, x + tile * 0.2, y + tile * 0.1, tile * 1.2);
        bg.addColorStop(0, 'hsl(' + h + ' 45% 32%)');
        bg.addColorStop(1, 'hsl(' + h + ' 25% 12%)');
        ctx.fillStyle = bg;
        ctx.fillRect(x, y, tile, tile);
        ctx.fillStyle = 'hsl(' + h + ' 60% 82%)';
        ctx.font = 'italic ' + Math.round(tile * 0.44) + 'px ' + SERIF;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(cell.dataset.initial, x + tile / 2, y + tile / 2);
      }

      if (names) {
        var shade = ctx.createLinearGradient(0, y + tile * 0.5, 0, y + tile);
        shade.addColorStop(0, 'rgba(0,0,0,0)');
        shade.addColorStop(1, 'rgba(0,0,0,0.85)');
        ctx.fillStyle = shade;
        ctx.fillRect(x, y + tile * 0.5, tile, tile * 0.5);

        var pad = tile * 0.07;
        var size = Math.max(11, Math.round(tile * 0.075));
        var lines = [[cell.dataset.name, '600 ' + size + 'px ' + SANS, '#fff']];
        if (cell.dataset.sub) lines.push([cell.dataset.sub, size * 0.85 + 'px ' + SANS, 'rgba(255,255,255,0.75)']);
        lines.push([cell.dataset.plays, size * 0.8 + 'px ' + SANS, 'rgba(255,255,255,0.6)']);
        ctx.textAlign = 'left';
        ctx.textBaseline = 'alphabetic';
        var ly = y + tile - pad;
        for (var i = lines.length - 1; i >= 0; i--) {
          ctx.font = lines[i][1];
          ctx.fillStyle = lines[i][2];
          ctx.fillText(fit(ctx, lines[i][0], tile - pad * 2), x + pad, ly);
          ly -= size * 1.25;
        }
      }
    }

    function renderGrid() {
      var n = Number(collage.dataset.size);
      var tile = Math.min(640, Math.floor(3000 / n));
      var names = collage.dataset.names === 'true';
      var cells = $$('.collage-cell', collage);
      var imgs = $$('img', collage);

      return Promise.all(imgs.map(whenLoaded)).then(function () {
        var canvas = document.createElement('canvas');
        canvas.width = canvas.height = n * tile;
        var ctx = canvas.getContext('2d');
        ctx.fillStyle = '#0d0e0c';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        cells.forEach(function (cell, i) {
          drawCell(ctx, cell, (i % n) * tile, Math.floor(i / n) * tile, tile, names);
        });
        return new Promise(function (resolve, reject) {
          try {
            canvas.toBlob(function (blob) {
              if (blob) resolve(blob); else reject(new Error('could not render the image'));
            }, 'image/png');
          } catch (err) {
            // an image served without CORS headers taints the canvas
            reject(new Error('an image could not be exported (its host blocks it)'));
          }
        });
      });
    }

    $('#gridSave').addEventListener('click', function () {
      busy(true);
      setGridStatus('Rendering…');
      renderGrid()
        .then(function (blob) {
          var link = document.createElement('a');
          link.href = URL.createObjectURL(blob);
          link.download = collage.dataset.filename;
          document.body.appendChild(link);
          link.click();
          link.remove();
          setTimeout(function () { URL.revokeObjectURL(link.href); }, 10000);
          setGridStatus('Saved ' + collage.dataset.filename, 'ok');
        })
        .catch(function (err) { setGridStatus('Could not save: ' + err.message, 'error'); })
        .finally(function () { busy(false); });
    });

    $('#gridCopy').addEventListener('click', function () {
      if (!window.isSecureContext || !navigator.clipboard || !window.ClipboardItem) {
        setGridStatus('Copying images needs HTTPS or localhost; use Save image instead.', 'error');
        return;
      }
      busy(true);
      setGridStatus('Rendering…');
      // pass the pending blob straight to the clipboard so Safari keeps the click's permission
      navigator.clipboard.write([new ClipboardItem({ 'image/png': renderGrid() })])
        .then(function () { setGridStatus('Copied to clipboard', 'ok'); })
        .catch(function (err) { setGridStatus('Could not copy: ' + err.message, 'error'); })
        .finally(function () { busy(false); });
    });
  }

  // ---------- settings tabs ----------

  var tabs = $$('.tab-button');
  function showTab(name, push) {
    var panel = document.getElementById(name);
    if (!panel || !panel.classList.contains('tab-panel')) return;
    tabs.forEach(function (b) { b.classList.toggle('active', b.dataset.tab === name); });
    $$('.tab-panel').forEach(function (p) { p.classList.toggle('active', p === panel); });
    if (push) {
      var url = new URL(window.location);
      url.searchParams.set('tab', name);
      history.replaceState(null, '', url);
    }
  }
  tabs.forEach(function (b) {
    b.addEventListener('click', function () { showTab(b.dataset.tab, true); });
  });
  if (query.get('tab')) showTab(query.get('tab'), false);
})();
