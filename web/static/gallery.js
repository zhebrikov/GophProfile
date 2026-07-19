(function () {
  const gallery = document.getElementById("gallery");
  const alertEl = document.getElementById("gallery-alert");
  if (!gallery) return;

  const userID = gallery.dataset.userId || "";

  function showAlert(message, ok) {
    if (!alertEl) return;
    alertEl.hidden = false;
    alertEl.textContent = message;
    alertEl.className = "alert " + (ok ? "alert-ok" : "alert-error");
  }

  gallery.addEventListener("click", function (e) {
    const btn = e.target.closest(".btn-delete");
    if (!btn) return;

    const avatarID = btn.dataset.avatarId;
    if (!avatarID || !userID) {
      showAlert("Не удалось определить User ID или аватар", false);
      return;
    }

    if (!window.confirm("Удалить эту аватарку?")) return;

    btn.disabled = true;

    fetch("/api/v1/avatars/" + encodeURIComponent(avatarID), {
      method: "DELETE",
      headers: { "X-User-ID": userID },
    })
      .then(function (res) {
        if (res.status === 204) {
          const item = gallery.querySelector(
            '.gallery-item[data-avatar-id="' + avatarID + '"]'
          );
          if (item) item.remove();
          showAlert("Аватарка удалена", true);
          if (!gallery.querySelector(".gallery-item")) {
            gallery.innerHTML =
              '<p class="empty">Пока нет аватарок. <a href="/web/upload">Загрузить первую</a></p>';
          }
          return;
        }
        return res.json().then(function (body) {
          var msg =
            (body && (body.error || body.details)) ||
            "Ошибка удаления (" + res.status + ")";
          throw new Error(msg);
        });
      })
      .catch(function (err) {
        showAlert(err.message || "Ошибка удаления", false);
        btn.disabled = false;
      });
  });
})();
