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
        if (value > maxLimit) {
            option.style.display = 'none';
        } else {
            option.style.display = '';
        }
    }
    
    if (parseInt(limitSelect.value) > maxLimit) {
        limitSelect.value = maxLimit;
    }
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
});
