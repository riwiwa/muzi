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

  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      closeMenu();
      closeEditModal();
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
      
      if (query.length < 1) {
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

    document.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        searchResults.classList.remove('active');
      }
    });

    document.addEventListener('click', function(e) {
      if (!searchInput.contains(e.target) && !searchResults.contains(e.target)) {
        searchResults.classList.remove('active');
      }
    });
  }

  // Image Upload Functionality
  document.querySelectorAll('.editable-image').forEach(function(img) {
    img.style.cursor = 'pointer';
    img.addEventListener('click', function(e) {
      var entityType = this.getAttribute('data-entity');
      var entityId = this.getAttribute('data-id');
      var field = this.getAttribute('data-field');
      
      var input = document.createElement('input');
      input.type = 'file';
      input.accept = 'image/jpeg,image/png,image/gif,image/webp';
      input.onchange = function(e) {
        var file = e.target.files[0];
        if (!file) return;
        
        if (file.size > 5 * 1024 * 1024) {
          alert('File exceeds 5MB limit');
          return;
        }
        
        var formData = new FormData();
        formData.append('file', file);
        
        var xhr = new XMLHttpRequest();
        xhr.open('POST', '/api/upload/image', true);
        xhr.onreadystatechange = function() {
          if (xhr.readyState === 4) {
            if (xhr.status === 200) {
              var result = JSON.parse(xhr.responseText);
              
              var patchXhr = new XMLHttpRequest();
              patchXhr.open('PATCH', '/api/' + entityType + '/' + entityId + '/edit?field=' + field, true);
              patchXhr.setRequestHeader('Content-Type', 'application/json');
              patchXhr.onreadystatechange = function() {
                if (patchXhr.readyState === 4) {
                  if (patchXhr.status === 200) {
                    img.src = result.url;
                  } else {
                    alert('Error updating image: ' + patchXhr.responseText);
                  }
                }
              };
              patchXhr.send(JSON.stringify({ value: result.url }));
            } else {
              alert('Error uploading: ' + xhr.responseText);
            }
          }
        };
        xhr.send(formData);
      };
      input.click();
    });
  });

  // Generic edit form handler
  var editForm = document.getElementById('editForm');
  if (editForm) {
    editForm.addEventListener('submit', function(e) {
      e.preventDefault();
      var form = e.target;
      var entityType = form.getAttribute('data-entity');
      var entityId = form.getAttribute('data-id');
      
      var data = {};
      var elements = form.querySelectorAll('input, textarea');
      for (var i = 0; i < elements.length; i++) {
        var el = elements[i];
        if (el.name) {
          data[el.name] = el.value;
        }
      }
      
      var xhr = new XMLHttpRequest();
      xhr.open('PATCH', '/api/' + entityType + '/' + entityId + '/batch', true);
      xhr.setRequestHeader('Content-Type', 'application/json');
      xhr.onreadystatechange = function() {
        if (xhr.readyState === 4) {
          if (xhr.status === 200) {
            var response = JSON.parse(xhr.responseText);
            if (response.success && entityType === 'song' && response.artist && response.title && response.username) {
              var newUrl = '/profile/' + response.username + '/song/' + encodeURIComponent(response.artist) + '/' + encodeURIComponent(response.title);
              window.location.href = newUrl;
            } else {
              location.reload();
            }
          } else {
            alert('Error saving: ' + xhr.responseText);
          }
        }
      };
      xhr.send(JSON.stringify(data));
    });
  }
});

function openEditModal() {
  document.getElementById('editModal').style.display = 'flex';
}

function closeEditModal() {
  document.getElementById('editModal').style.display = 'none';
}
