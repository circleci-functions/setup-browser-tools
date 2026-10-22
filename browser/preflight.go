package browser

import "fmt"

// requirePrivilege fails up front when step needs root and the job has neither
// root nor sudo. Without it the step runs unprefixed and fails later, after the
// downloads, with an error from the system rather than one naming what is
// missing.
func (i *Installer) requirePrivilege(component, step string) error {
	if i.Runner.Priv.Escalated() {
		return nil
	}
	return fmt.Errorf("%s needs root or sudo to %s, and this job is not root and has no sudo", component, step)
}

// requireWritable is requirePrivilege for an install that only writes files:
// directories the job user can write to need neither root nor sudo, so a driver
// can still go into a writable --install-dir.
func (i *Installer) requireWritable(component string, dirs ...string) error {
	if i.Runner.Priv.Escalated() {
		return nil
	}
	for _, dir := range dirs {
		if !i.Host.writable(dir) {
			return fmt.Errorf("%s needs root or sudo to write to %s, and this job is not root and has no sudo", component, dir)
		}
	}
	return nil
}

// packageManager names the tool a Linux install runs, for error messages.
func (i *Installer) packageManager() string {
	switch i.Platform.Distro {
	case DistroAlpine:
		return "apk"
	case DistroRHEL:
		return "yum"
	default:
		return "apt-get"
	}
}

func (i *Installer) chromeInstaller() string {
	if i.Platform.IsMacOS() {
		return "/usr/sbin/installer"
	}
	return i.packageManager()
}
