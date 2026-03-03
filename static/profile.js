function updateTopArtists() {
    const period = document.getElementById('period-select').value;
    const limit = document.getElementById('limit-select').value;
    const view = document.getElementById('view-select').value;
    
    const customDates = document.getElementById('custom-dates');
    if (period === 'custom') {
        customDates.style.display = 'inline-block';
    } else {
        customDates.style.display = 'none';
    }
    
    const params = new URLSearchParams(window.location.search);
    params.set('period', period);
    params.set('limit', limit);
    params.set('view', view);
    
    if (period === 'custom') {
        const startDate = document.getElementById('start-date').value;
        const endDate = document.getElementById('end-date').value;
        if (startDate) params.set('start', startDate);
        if (endDate) params.set('end', endDate);
    }
    
    window.location.search = params.toString();
}

function updateLimitOptions() {
    const view = document.getElementById('view-select').value;
    const limitSelect = document.getElementById('limit-select');
    const maxLimit = view === 'grid' ? 8 : 30;
    
    for (let option of limitSelect.options) {
        const value = parseInt(option.value);
        if (value > maxLimit || (view === 'grid' && value === 7)) {
            option.style.display = 'none';
        } else {
            option.style.display = '';
        }
    }
    
    if (parseInt(limitSelect.value) > maxLimit || (view === 'grid' && parseInt(limitSelect.value) === 7)) {
        limitSelect.value = maxLimit;
    }
}

function updateTopAlbums() {
    const period = document.getElementById('album-period-select').value;
    const limit = document.getElementById('album-limit-select').value;
    const view = document.getElementById('album-view-select').value;
    
    const customDates = document.getElementById('album-custom-dates');
    if (period === 'custom') {
        customDates.style.display = 'inline-block';
    } else {
        customDates.style.display = 'none';
    }
    
    const params = new URLSearchParams(window.location.search);
    params.set('album_period', period);
    params.set('album_limit', limit);
    params.set('album_view', view);
    
    if (period === 'custom') {
        const startDate = document.getElementById('album-start-date').value;
        const endDate = document.getElementById('album-end-date').value;
        if (startDate) params.set('album_start', startDate);
        if (endDate) params.set('album_end', endDate);
    }
    
    window.location.search = params.toString();
}

function updateTopAlbumsLimitOptions() {
    const view = document.getElementById('album-view-select').value;
    const limitSelect = document.getElementById('album-limit-select');
    const maxLimit = view === 'grid' ? 8 : 30;
    
    for (let option of limitSelect.options) {
        const value = parseInt(option.value);
        if (value > maxLimit || (view === 'grid' && value === 7)) {
            option.style.display = 'none';
        } else {
            option.style.display = '';
        }
    }
    
    if (parseInt(limitSelect.value) > maxLimit || (view === 'grid' && parseInt(limitSelect.value) === 7)) {
        limitSelect.value = maxLimit;
    }
}

function updateTopTracks() {
    const period = document.getElementById('track-period-select').value;
    const limit = document.getElementById('track-limit-select').value;
    
    const customDates = document.getElementById('track-custom-dates');
    if (period === 'custom') {
        customDates.style.display = 'inline-block';
    } else {
        customDates.style.display = 'none';
    }
    
    const params = new URLSearchParams(window.location.search);
    params.set('track_period', period);
    params.set('track_limit', limit);
    
    if (period === 'custom') {
        const startDate = document.getElementById('track-start-date').value;
        const endDate = document.getElementById('track-end-date').value;
        if (startDate) params.set('track_start', startDate);
        if (endDate) params.set('track_end', endDate);
    }
    
    window.location.search = params.toString();
}

function syncGridHeights() {}

document.addEventListener('DOMContentLoaded', function() {
    const customDates = document.getElementById('custom-dates');
    const periodSelect = document.getElementById('period-select');
    
    if (periodSelect && customDates) {
        if (periodSelect.value === 'custom') {
            customDates.style.display = 'inline-block';
        }
    }
    
    updateLimitOptions();
    
    updateTopAlbumsLimitOptions();
});
