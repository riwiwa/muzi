function handleImport(formId, progressPrefix, endpoint, progressUrl, formatLabel) {
  const form = document.getElementById(formId);
  const progressContainer = document.getElementById(progressPrefix + '-progress');
  const progressFill = document.getElementById(progressPrefix + '-progress-fill');
  const progressText = document.getElementById(progressPrefix + '-progress-text');
  const progressStatus = document.getElementById(progressPrefix + '-progress-status');
  const progressTracks = document.getElementById(progressPrefix + '-progress-tracks');
  const progressError = document.getElementById(progressPrefix + '-progress-error');
  const progressSuccess = document.getElementById(progressPrefix + '-progress-success');

  form.addEventListener('submit', async function(e) {
    e.preventDefault();

    // Reset and show progress
    progressFill.style.width = '0%';
    progressFill.classList.add('animating');
    progressText.textContent = '0%';
    progressStatus.textContent = 'Starting import...';
    progressTracks.textContent = '';
    progressError.textContent = '';
    progressSuccess.textContent = '';
    progressContainer.style.display = 'block';

    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        body: progressPrefix === 'lastfm' 
          ? new URLSearchParams(new FormData(form))
          : new FormData(form)
      });

      if (!response.ok) throw new Error('Failed to start import: ' + response.statusText);

      const { job_id } = await response.json();
      const eventSource = new EventSource(progressUrl + job_id);

      eventSource.onmessage = function(event) {
        const update = JSON.parse(event.data);
        if (update.status === 'connected') return;

        if (update.total_pages > 0) {
          const completed = update.completed_pages || update.current_page || 0;
          const percent = Math.round((completed / update.total_pages) * 100);
          progressFill.style.width = percent + '%';
          progressText.textContent = percent + '%';
          progressStatus.textContent = 'Processing ' + formatLabel + ' ' + completed + ' of ' + update.total_pages;
        }

        if (update.tracks_imported !== undefined) {
          progressTracks.textContent = update.tracks_imported.toLocaleString() + ' tracks imported';
        }

        if (update.status === 'completed') {
          progressFill.classList.remove('animating');
          progressStatus.textContent = 'Import completed!';
          progressSuccess.textContent = 'Successfully imported ' + update.tracks_imported.toLocaleString() + ' tracks from ' + (progressPrefix === 'spotify' ? 'Spotify' : 'Last.fm');
          eventSource.close();
          form.reset();
        } else if (update.status === 'error') {
          progressFill.classList.remove('animating');
          progressStatus.textContent = 'Import failed';
          progressError.textContent = 'Error: ' + (update.error || 'Unknown error');
          eventSource.close();
        }
      };

      eventSource.onerror = function() {
        progressFill.classList.remove('animating');
        progressStatus.textContent = 'Connection error';
        progressError.textContent = 'Lost connection to server. The import may still be running in the background.';
        eventSource.close();
      };

    } catch (err) {
      progressFill.classList.remove('animating');
      progressStatus.textContent = 'Import failed';
      progressError.textContent = 'Error: ' + err.message;
    }
  });
}

handleImport('spotify-form', 'spotify', '/import/spotify', '/import/spotify/progress?job=', 'batch');
handleImport('lastfm-form', 'lastfm', '/import/lastfm', '/import/lastfm/progress?job=', 'page');
