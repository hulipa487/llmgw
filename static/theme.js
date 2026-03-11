/**
 * Theme Management Script
 * Handles dark/light mode toggle with localStorage persistence
 */

(function() {
    'use strict';

    const THEME_KEY = 'theme-preference';
    const DARK_THEME = 'dark';
    const LIGHT_THEME = 'light';

    /**
     * Get the current theme preference
     * Priority: localStorage > system preference > light (default)
     */
    function getThemePreference() {
        const stored = localStorage.getItem(THEME_KEY);
        if (stored) {
            return stored;
        }
        // Check system preference
        if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
            return DARK_THEME;
        }
        return LIGHT_THEME;
    }

    /**
     * Apply theme to the document
     */
    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        localStorage.setItem(THEME_KEY, theme);
    }

    /**
     * Toggle between light and dark themes
     */
    function toggleTheme() {
        const current = document.documentElement.getAttribute('data-theme') || LIGHT_THEME;
        const newTheme = current === DARK_THEME ? LIGHT_THEME : DARK_THEME;
        applyTheme(newTheme);
    }

    /**
     * Create and insert theme toggle button into navbar
     */
    function createThemeToggle() {
        const navLinks = document.querySelector('.nav-links');
        if (!navLinks) return;

        // Check if toggle already exists
        if (document.querySelector('.theme-toggle')) return;

        const toggleBtn = document.createElement('button');
        toggleBtn.className = 'theme-toggle';
        toggleBtn.setAttribute('aria-label', 'Toggle dark/light theme');
        toggleBtn.innerHTML = `
            <svg class="moon-icon" viewBox="0 0 24 24" fill="currentColor">
                <path d="M21.752 15.002A9.718 9.718 0 0118 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 003 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 009.002-5.998z"/>
            </svg>
            <svg class="sun-icon" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 2.25a.75.75 0 01.75.75v2.25a.75.75 0 01-1.5 0V3a.75.75 0 01.75-.75zM7.5 12a4.5 4.5 0 119 0 4.5 4.5 0 01-9 0zM18.894 6.166a.75.75 0 00-1.06-1.06l-1.591 1.59a.75.75 0 101.06 1.061l1.591-1.59zM21.75 12a.75.75 0 01-.75.75h-2.25a.75.75 0 010-1.5H21a.75.75 0 01.75.75zM17.834 18.894a.75.75 0 001.06-1.06l-1.59-1.591a.75.75 0 10-1.061 1.06l1.59 1.591zM12 18a.75.75 0 01.75.75V21a.75.75 0 01-1.5 0v-2.25A.75.75 0 0112 18zM7.758 17.303a.75.75 0 00-1.061-1.06l-1.591 1.59a.75.75 0 001.06 1.061l1.591-1.59zM6 12a.75.75 0 01-.75.75H3a.75.75 0 010-1.5h2.25A.75.75 0 016 12zM6.697 7.757a.75.75 0 001.06-1.06l-1.59-1.591a.75.75 0 00-1.061 1.06l1.59 1.591z"/>
            </svg>
        `;
        toggleBtn.addEventListener('click', toggleTheme);

        navLinks.appendChild(toggleBtn);
    }

    /**
     * Initialize theme on page load
     */
    function initTheme() {
        // Apply saved theme immediately to prevent flash
        const theme = getThemePreference();
        applyTheme(theme);

        // Create toggle button after DOM is ready
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', createThemeToggle);
        } else {
            createThemeToggle();
        }

        // Listen for system theme changes
        if (window.matchMedia) {
            window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
                // Only auto-switch if user hasn't manually set preference
                if (!localStorage.getItem(THEME_KEY)) {
                    applyTheme(e.matches ? DARK_THEME : LIGHT_THEME);
                }
            });
        }
    }

    // Apply theme immediately (before DOM ready) to prevent flash
    const initialTheme = getThemePreference();
    document.documentElement.setAttribute('data-theme', initialTheme);

    // Initialize toggle on DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', createThemeToggle);
    } else {
        createThemeToggle();
    }

    // Expose toggle function globally
    window.toggleTheme = toggleTheme;
    window.getTheme = () => document.documentElement.getAttribute('data-theme') || LIGHT_THEME;
})();