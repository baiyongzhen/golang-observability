# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|

  if (/cygwin|mswin|mingw|bccwin|wince|emx/ =~ RUBY_PLATFORM) != nil
    #config.vm.synced_folder ".", "/vagrant", mount_options: ["dmode=700,fmode=600"]
    config.vm.synced_folder ".", "/vagrant", mount_options: ["dmode=777,fmode=777"]
  else
    config.vm.synced_folder ".", "/vagrant"
  end


  config.vm.define :golang do |host|
    host.vm.box = "bento/ubuntu-18.04"
    host.vm.hostname = "golang"
    host.vm.network :private_network, ip: "192.168.56.126"
    host.vm.provision :shell, path: "scripts/debian_bootstrap.sh"
    # boot timeout
    host.vm.boot_timeout = 300

    # Set system settings
    host.vm.provider :virtualbox do |vb|
        vb.customize ["modifyvm", :id, "--memory", "3072"]
        vb.customize ["modifyvm", :id, "--cpus", "2"]
        vb.customize ['modifyvm', :id, '--cableconnected1', 'on']
    end
  end  

end