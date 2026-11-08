import '@material/web/button/filled-button.js';
import '@material/web/button/filled-tonal-button.js';
import '@material/web/button/outlined-button.js';
import '@material/web/button/text-button.js';
import '@material/web/iconbutton/icon-button.js';
import '@material/web/icon/icon.js';
import '@material/web/chips/chip-set.js';
import '@material/web/chips/assist-chip.js';
import '@material/web/tabs/tabs.js';
import '@material/web/tabs/primary-tab.js';
import '@material/web/tabs/secondary-tab.js';
import '@material/web/textfield/outlined-text-field.js';
import '@material/web/select/outlined-select.js';
import '@material/web/select/select-option.js';
import '@material/web/radio/radio.js';
import '@material/web/switch/switch.js';
import '@material/web/list/list.js';
import '@material/web/list/list-item.js';
import '@material/web/progress/linear-progress.js';
import '@material/web/progress/circular-progress.js';
import '@material/web/dialog/dialog.js';
import '@material/web/divider/divider.js';
import {styles} from '@material/web/typography/md-typescale-styles.js';

document.adoptedStyleSheets.push(styles.styleSheet);
// App initialization runs only after all Material elements have been registered.
const app = document.createElement('script');
app.src = 'app.js';
document.head.append(app);
