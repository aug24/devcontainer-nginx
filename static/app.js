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
    const pasteButton = document.getElementById('paste-and-save-button');
    const quickButton = document.getElementById('quick-paste-and-shell-button');
    if (!form) {
      return;
    }

    function inferProfileFromBootstrapCommand(text) {
      if (!text || typeof text !== 'string') {
        return '';
      }

      const patterns = [
        /aws\s+configure\s+set\s+--profile\s+([A-Za-z0-9._-]+)/i,
        /(?:--profile|--profile=)\s*([A-Za-z0-9._-]+)/i,
        /(?:AWS_PROFILE|aws_profile)\s*(?:=|:)\s*["']?([A-Za-z0-9._-]+)/i,
        /(?:export\s+)?AWS_PROFILE\s*=\s*["']?([A-Za-z0-9._-]+)/i,
      ];

      for (const pattern of patterns) {
        const match = text.match(pattern);
        if (match && match[1]) {
          return match[1];
        }
      }

      return '';
    }

    function clearBootstrapCommand() {
      const textarea = form.querySelector('textarea[name="bootstrapCommand"]');
      if (textarea) {
        textarea.value = '';
      }
    }

    function normalizeBootstrapCommandText(value) {
      return (value || '').toString().trim();
    }

    function looksLikeAWSBootstrapCommand(value) {
      const trimmed = normalizeBootstrapCommandText(value);
      if (!trimmed) {
        return true;
      }
      return trimmed.toLowerCase().startsWith('aws configure');
    }

    async function submitForm({ openShellAfterSave = false, popup = null } = {}) {
      const formData = new FormData(form);
      const bootstrapCommand = normalizeBootstrapCommandText(formData.get('bootstrapCommand'));
      if (bootstrapCommand && !looksLikeAWSBootstrapCommand(bootstrapCommand)) {
        console.warn('[quick-shell] rejected bootstrap command that does not start with aws configure', { bootstrapCommandPreview: bootstrapCommand.slice(0, 80) });
        alert('Clipboard contents must begin with "aws configure" after trimming whitespace.');
        return false;
      }
      const payload = new URLSearchParams({
        bootstrapCommand,
      });

      console.log('[quick-shell] submitForm start', {
        openShellAfterSave,
        bootstrapCommandLength: bootstrapCommand.length,
        popupExists: !!popup,
      });

      const response = await fetch('/aws-config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
        body: payload,
      });

      console.log('[quick-shell] submitForm response', {
        status: response.status,
        ok: response.ok,
        redirected: response.redirected,
        url: response.url,
      });

      if (!response.ok) {
        const text = await response.text();
        console.error('[quick-shell] submitForm failed', { status: response.status });
        alert(text || 'failed');
        return false;
      }

      clearBootstrapCommand();

      if (openShellAfterSave) {
        const profile = inferProfileFromBootstrapCommand(bootstrapCommand);
        console.log('[quick-shell] profile inference', { profile, bootstrapCommandLength: bootstrapCommand.length });
        if (!profile) {
          console.warn('[quick-shell] no profile inferred from bootstrap command');
          alert('No AWS profile could be inferred from the pasted command.');
          if (popup) {
            popup.close();
          }
          return false;
        }
        const query = new URLSearchParams({
          profile,
          region: 'eu-west-1',
        });
        const url = '/shell?' + query.toString();
        console.log('[quick-shell] opening shell URL', { url, profile, region: 'eu-west-1' });
        if (popup) {
          popup.location.href = url;
          popup.focus();
          console.log('[quick-shell] shell URL assigned to popup', { popupLocation: popup.location.href });
        } else {
          const opened = window.open(url, '_blank');
          console.log('[quick-shell] window.open result', { opened: !!opened, url });
        }
        return true;
      }

      console.log('[quick-shell] normal save; redirecting to /aws-config');
      window.location.href = '/aws-config';
      return true;
    }

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      await submitForm();
    });

    if (pasteButton) {
      pasteButton.addEventListener('click', async () => {
        console.log('[quick-shell] paste-and-save click');
        try {
          const text = await navigator.clipboard.readText();
          console.log('[quick-shell] clipboard read for paste-and-save', { length: text.length });
          if (!looksLikeAWSBootstrapCommand(text)) {
            console.warn('[quick-shell] clipboard rejected for paste-and-save', { preview: text.slice(0, 80) });
            alert('Clipboard contents must begin with "aws configure" after trimming whitespace.');
            return;
          }
          const textarea = form.querySelector('textarea[name="bootstrapCommand"]');
          if (textarea) {
            textarea.value = text;
            console.log('[quick-shell] textarea populated from clipboard');
          }
          await submitForm();
        } catch (_error) {
          console.error('[quick-shell] paste-and-save failed', _error);
          alert('Unable to read clipboard contents.');
        }
      });
    }

    const quickButtonTargets = Array.from(
      new Set([
        document.getElementById('quick-paste-and-shell-button'),
        document.querySelector('[data-action="quick-paste-and-shell"]'),
      ].filter(Boolean))
    );

    quickButtonTargets.forEach((button) => {
      if (button.dataset.quickShellBound === 'true') {
        return;
      }
      button.dataset.quickShellBound = 'true';
      button.addEventListener('click', async () => {
        console.log('[quick-shell] quick button clicked');
        const shellTab = window.open('', '_blank');
        console.log('[quick-shell] shell tab created', { shellTabExists: !!shellTab, url: 'about:blank' });
        if (!shellTab) {
          console.warn('[quick-shell] new tab was blocked before save');
          alert('Opening a new shell tab was blocked. Please allow new tabs for this page and try again.');
          return;
        }

        try {
          const text = await navigator.clipboard.readText();
          console.log('[quick-shell] clipboard read for quick action', { length: text.length });
          if (!looksLikeAWSBootstrapCommand(text)) {
            console.warn('[quick-shell] clipboard rejected for quick action', { preview: text.slice(0, 80) });
            alert('Clipboard contents must begin with "aws configure" after trimming whitespace.');
            shellTab.close();
            return;
          }
          const textarea = form.querySelector('textarea[name="bootstrapCommand"]');
          if (textarea) {
            textarea.value = text;
            console.log('[quick-shell] textarea populated for quick action');
          }

          const ok = await submitForm({ openShellAfterSave: true, popup: shellTab });
          if (!ok) {
            console.log('[quick-shell] quick action did not complete successfully');
            return;
          }
        } catch (_error) {
          console.error('[quick-shell] quick action failed', _error);
          alert('Unable to read clipboard contents.');
          if (shellTab) {
            shellTab.close();
          }
        }
      });
    });

    if (quickButton) {
      quickButton.dataset.quickShellBound = 'true';
    }
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

    document.querySelectorAll('[data-start]').forEach((button) => {
      button.addEventListener('click', async function () {
        const id = this.getAttribute('data-start');
        const response = await fetch('/api/tunnels/start/' + encodeURIComponent(id), {
          method: 'POST',
        });

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

      const secondaryFields = document.querySelectorAll('[data-command-secondary]');
      secondaryFields.forEach((field) => {
        const parent = field.closest('.command-field');
        const isDisabled = command === 'bash';
        if (parent) {
          parent.classList.toggle('is-disabled', isDisabled);
          parent.classList.toggle('is-active', !isDisabled);
        }
        field.disabled = isDisabled;
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

      if (command === 'bash') {
        parts.push('-it', '-e', `AWS_PROFILE=${profile || 'frontend'}`, '-e', `AWS_REGION=${region || 'eu-west-1'}`, container, 'bash');
      } else {
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

  function setupRegionControls() {
    const controls = document.querySelectorAll('.region-control');
    if (!controls.length) {
      return;
    }

    const predefined = new Set(['eu-west-1', 'us-east-1', 'us-east-2', 'eu-west-2', 'us-west-2']);

    controls.forEach((control) => {
      const select = control.querySelector('.region-select');
      const input = control.querySelector('.region-input');
      if (!select || !input) {
        return;
      }

      const syncSelectFromInput = () => {
        const value = (input.value || '').trim();
        if (!value) {
          select.value = 'eu-west-1';
          return;
        }
        select.value = predefined.has(value) ? value : 'custom';
      };

      const syncInputFromSelect = () => {
        const value = select.value;
        if (value === 'custom') {
          return;
        }
        input.value = value;
      };

      input.addEventListener('input', syncSelectFromInput);
      input.addEventListener('change', syncSelectFromInput);
      select.addEventListener('change', syncInputFromSelect);
      syncSelectFromInput();
    });
  }

  document.addEventListener('DOMContentLoaded', () => {
    setupRegionControls();

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
