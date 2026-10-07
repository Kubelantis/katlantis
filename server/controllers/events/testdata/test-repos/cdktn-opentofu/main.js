const { App, TerraformStack, TerraformOutput, LocalBackend } = require("cdktn");

class Net extends TerraformStack {
  constructor(scope, id) {
    super(scope, id);
    new LocalBackend(this, { path: `${id}.tfstate` });
    new TerraformOutput(this, "engine", { value: `${id}-on-opentofu` });
  }
}

const app = new App();
new Net(app, "net");
app.synth();
