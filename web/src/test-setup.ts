import "@testing-library/jest-dom/vitest";
window.scrollTo = () => {};

// jsdom n'implémente pas <dialog>
HTMLDialogElement.prototype.showModal = function () { this.setAttribute("open", ""); };
HTMLDialogElement.prototype.close = function () { this.removeAttribute("open"); this.dispatchEvent(new Event("close")); };
