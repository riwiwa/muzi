document.addEventListener('DOMContentLoaded', function() {
  const menuButton = document.getElementById('menuButton');
  const sideMenu = document.getElementById('sideMenu');
  const menuOverlay = document.getElementById('menuOverlay');

  function toggleMenu() {
    menuButton.classList.toggle('active');
    sideMenu.classList.toggle('active');
    menuOverlay.classList.toggle('active');
  }

  function closeMenu() {
    menuButton.classList.remove('active');
    sideMenu.classList.remove('active');
    menuOverlay.classList.remove('active');
  }

  if (menuButton) {
    menuButton.addEventListener('click', toggleMenu);
  }

  if (menuOverlay) {
    menuOverlay.addEventListener('click', closeMenu);
  }

  // Close menu on escape key
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      closeMenu();
    }
  });

  // Global Search
  const searchInput = document.getElementById('globalSearch');
  const searchResults = document.getElementById('searchResults');
  let searchTimeout;

  if (searchInput) {
    searchInput.addEventListener('input', function(e) {
      const query = e.target.value.trim();
      
      clearTimeout(searchTimeout);
      
      if (query.length < 2) {
        searchResults.classList.remove('active');
        searchResults.innerHTML = '';
        return;
      }

      searchTimeout = setTimeout(function() {
        var xhr = new XMLHttpRequest();
        xhr.open('GET', '/search?q=' + encodeURIComponent(query), true);
        xhr.onreadystatechange = function() {
          if (xhr.readyState === 4) {
            if (xhr.status === 200) {
              var results = JSON.parse(xhr.responseText);
              if (results.length === 0) {
                searchResults.innerHTML = '<div class="search-result-item"><span class="search-result-name">No results</span></div>';
              } else {
                var html = '';
                for (var i = 0; i < results.length; i++) {
                  var r = results[i];
                  html += '<a href="' + r.url + '" class="search-result-item">' +
                    '<div class="search-result-info">' +
                      '<span class="search-result-name">' + r.name + '</span>' +
                      '<span class="search-result-type">' + r.type + '</span>' +
                    '</div>' +
                    '<span class="search-result-count">' + r.count + '</span>' +
                  '</a>';
                }
                searchResults.innerHTML = html;
              }
              searchResults.classList.add('active');
            }
          }
        };
        xhr.send();
      }, 300);
    });

    // Close search on escape
    document.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        searchResults.classList.remove('active');
      }
    });

    // Close search when clicking outside
    document.addEventListener('click', function(e) {
      if (!searchInput.contains(e.target) && !searchResults.contains(e.target)) {
        searchResults.classList.remove('active');
      }
    });
  }
});
