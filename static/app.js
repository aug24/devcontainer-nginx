(function () {
  function setupHomepage() {
    const labels = document.querySelectorAll('.container-label');
    if (!labels.length) {
      return;
    }

    labels.forEach((label) => {
      const toggle = (event) => {
        if (event && typeof event.stopPropagation === 'function') {
          event.stopPropagation();
        }

        const isOpen = label.classList.contains('open');
        document.querySelectorAll('.container-label').forEach((other) => {
          if (other !== label) {
            other.classList.remove('open');
          }
        });

        label.classList.toggle('open', !isOpen);
      };

      label.addEventListener('click', toggle);
      label.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          toggle(event);
        }
      });
    });

    document.addEventListener('click', (event) => {
      document.querySelectorAll('.container-label').forEach((label) => {
        if (!label.contains(event.target)) {
          label.classList.remove('open');
        }
      });
    });

    const body = document.body;
    const revision = Number(body.dataset.currentRevision || '0');
    if (!Number.isFinite(revision)) {
      return;
    }

    async function checkForUpdate() {
      try {
        const response = await fetch('/homepage/version', { cache: 'no-store' });
        if (!response.ok) {
          return;
        }

        const next = Number((await response.text()).trim());
        if (Number.isFinite(next) && next > revision) {
          location.reload();
        }
      } catch (_error) {
        // Ignore polling failures and keep the page available.
      }
    }

    setInterval(checkForUpdate, 2000);
  }

  function setupAWSConfig() {
    const form = document.getElementById('aws-form');
    if (!form) {
      return;
    }

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(event.target);
      const payload = {
        bootstrapCommand: (formData.get('bootstrapCommand') || '').toString(),
      };

      const response = await fetch('/api/aws-config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        alert((await response.text()) || 'failed');
        return;
      }

      location.reload();
    });
  }

  function setupTunnelPage() {
    const form = document.getElementById('tunnel-form');
    if (form) {
      form.addEventListener('submit', async (event) => {
        event.preventDefault();
        const formData = new FormData(event.target);
        const payload = {
          name: (formData.get('name') || '').toString(),
          profile: (formData.get('profile') || '').toString(),
          region: (formData.get('region') || '').toString(),
          stack: (formData.get('stack') || '').toString(),
          stage: (formData.get('stage') || '').toString(),
          app: (formData.get('app') || '').toString(),
          alb: (formData.get('alb') || '').toString(),
          upstreamProtocol: formData.get('https') === 'on' ? 'https' : 'http',
          verifySSL: formData.get('verifySSL') === 'on',
        };

        const response = await fetch('/api/tunnels', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });

        if (!response.ok) {
          alert((await response.text()) || 'failed');
          return;
        }

        location.reload();
      });
    }

    document.querySelectorAll('[data-delete]').forEach((button) => {
      button.addEventListener('click', async function () {
        const id = this.getAttribute('data-delete');
        const response = await fetch('/api/tunnels/' + encodeURIComponent(id), { method: 'DELETE' });
        if (!response.ok) {
          alert((await response.text()) || 'failed');
          return;
        }

        location.reload();
      });
    });

    document.querySelectorAll('[data-stop]').forEach((button) => {
      button.addEventListener('click', async function () {
        const id = this.getAttribute('data-stop');
        const response = await fetch('/api/tunnels/stop/' + encodeURIComponent(id), {
          method: 'POST',
        });

        if (!response.ok) {
          alert((await response.text()) || 'failed');
          return;
        }

        location.reload();
      });
    });
  }

  function setupCommandGenerator() {
    const form = document.getElementById('command-form');
    const output = document.getElementById('generated-command');
    const copyButton = document.getElementById('copy-button');
    const tunnelFields = document.querySelectorAll('[id^="host-tunnel-fields"]');

    if (!form || !output || !copyButton) {
      return;
    }

    function updateVisibility() {
      const command = form.elements.command.value;
      const show = command === 'host-tunnel';
      tunnelFields.forEach((element) => {
        element.classList.toggle('hidden', !show);
      });
    }

    function render() {
      const data = new FormData(form);
      const container = (data.get('container') || 'devcontainer-nginx').toString().trim() || 'devcontainer-nginx';
      const user = data.get('user') || 'root';
      const command = (data.get('command') || 'shell').toString().trim();
      const profile = (data.get('profile') || '').toString().trim();
      const stack = (data.get('stack') || '').toString().trim();
      const stage = (data.get('stage') || '').toString().trim();
      const app = (data.get('app') || '').toString().trim();
      const region = (data.get('region') || '').toString().trim();
      const alb = (data.get('alb') || '').toString().trim();
      const localPort = (data.get('localPort') || '').toString().trim();
      const name = (data.get('name') || '').toString().trim();
      const parts = ['docker', 'exec'];

      if (user === 'root') {
        parts.push('-u', 'root');
      }

      parts.push('-it', container, command);

      if (profile) {
        parts.push('--profile', profile);
      }

      if (command === 'host-tunnel') {
        if (stack) parts.push('--stack', stack);
        if (stage) parts.push('--stage', stage);
        if (app) parts.push('--app', app);
        if (region) parts.push('--region', region);
        if (alb) parts.push('--alb', alb);
        if (localPort) parts.push('--local-port', localPort);
        if (name) parts.push('--name', name);
      } else {
        if (stack) parts.push('--stack', stack);
        if (stage) parts.push('--stage', stage);
        if (app) parts.push('--app', app);
        if (region) parts.push('--region', region);
      }

      output.textContent = parts.join(' ');
    }

    form.addEventListener('input', () => {
      updateVisibility();
      render();
    });

    form.addEventListener('change', () => {
      updateVisibility();
      render();
    });

    copyButton.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(output.textContent);
        copyButton.textContent = 'Copied';
        setTimeout(() => {
          copyButton.textContent = 'Copy command';
        }, 1200);
      } catch (_error) {
        copyButton.textContent = 'Copy failed';
      }
    });

    updateVisibility();
    render();
  }

  document.addEventListener('DOMContentLoaded', () => {
    const page = document.body.dataset.page;

    if (page === 'homepage') {
      setupHomepage();
    }

    if (page === 'aws-config') {
      setupAWSConfig();
    }

    if (page === 'tunnel-page') {
      setupTunnelPage();
    }

    if (page === 'command-generator') {
      setupCommandGenerator();
    }
  });
})();

