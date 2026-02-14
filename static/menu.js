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
});
